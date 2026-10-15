import { DurableObject } from 'cloudflare:workers';
import { z } from 'zod';
import { oauthRequest, revokeSubscription } from './subscription-oauth.js';
import type { Bindings } from './config.js';
import { SubscriptionCredential, SubscriptionImport } from './subscription-schema.js';

type Session = {
  sessionId: string;
  hostId: string;
  expires: number;
  encrypted: { iv: number[]; data: number[] };
  refreshing: boolean;
  requiresLogin: boolean;
};
const TokenResponse = z.object({
  access_token: z.string().min(1),
  refresh_token: z.string().min(1),
  token_type: z.string().regex(/^Bearer$/i),
  expires_in: z.number().positive(),
  scope: z.string().optional(),
  id_token: z.string().optional(),
});
const loginRequired = () => new Error('Cloud subscription needs login. Run romp cloud login');

export class SubscriptionAuth extends DurableObject<Bindings> {
  #chain: Promise<unknown> = Promise.resolve();

  #serialize<T>(operation: () => Promise<T>): Promise<T> {
    const pending = this.#chain.then(operation);
    this.#chain = pending.catch(() => {});
    return pending;
  }
  async #key() {
    const value = this.env.SUBSCRIPTION_KEY;
    if (!value || !/^[a-f0-9]{64}$/.test(value))
      throw new Error('SUBSCRIPTION_KEY must contain 32 random bytes in hexadecimal');
    return crypto.subtle.importKey(
      'raw',
      Uint8Array.from(value.match(/../g) ?? [], (byte) => parseInt(byte, 16)),
      'AES-GCM',
      false,
      ['encrypt', 'decrypt'],
    );
  }
  async #encrypt(credential: SubscriptionCredential) {
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const data = await crypto.subtle.encrypt(
      { name: 'AES-GCM', iv },
      await this.#key(),
      new TextEncoder().encode(JSON.stringify(credential)),
    );
    return { iv: Array.from(iv), data: Array.from(new Uint8Array(data)) };
  }
  async #decrypt(session: Session) {
    try {
      const bytes = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: new Uint8Array(session.encrypted.iv) },
        await this.#key(),
        new Uint8Array(session.encrypted.data),
      );
      return SubscriptionCredential.parse(JSON.parse(new TextDecoder().decode(bytes)) as unknown);
    } catch {
      throw loginRequired();
    }
  }
  async #readStatus() {
    const session = await this.ctx.storage.get<Session>('session');
    return session
      ? {
          configured: true,
          requiresLogin: session.requiresLogin || session.refreshing,
          sessionId: session.sessionId,
          hostId: session.hostId,
          expires: session.expires,
        }
      : { configured: false, requiresLogin: true };
  }
  status() {
    return this.#serialize(() => this.#readStatus());
  }

  importSession(value: unknown) {
    return this.#serialize(async () => {
      const input = SubscriptionImport.parse(value);
      const previous = await this.ctx.storage.get<Session>('session');
      if (await this.ctx.storage.get(`import:${input.sessionId}`)) {
        if (previous?.sessionId !== input.sessionId)
          throw new Error('This cloud login was replaced');
        return this.#readStatus();
      }
      const hostId = await this.ctx.storage.get<string>('host');
      if (hostId && hostId !== input.hostId)
        throw new Error('Cloud host identity changed. Use the original cloud login profile');
      if (input.credential.expires <= Date.now()) throw loginRequired();
      const session: Session = {
        sessionId: input.sessionId,
        hostId: input.hostId,
        expires: input.credential.expires,
        encrypted: await this.#encrypt(input.credential),
        refreshing: false,
        requiresLogin: false,
      };
      await this.ctx.storage.put({
        session,
        host: input.hostId,
        [`import:${input.sessionId}`]: true,
      });
      return this.#readStatus();
    });
  }
  access() {
    return this.#serialize(async () => {
      const session = await this.ctx.storage.get<Session>('session');
      if (!session || session.requiresLogin || session.refreshing) throw loginRequired();
      let credential = await this.#decrypt(session);
      if (credential.expires > Date.now() + 60_000) return credential.access;
      // Persist before exchange: an interrupted rotation must never replay its refresh token.
      session.refreshing = true;
      await this.ctx.storage.put('session', session);
      try {
        const token = await oauthRequest(
          'https://auth.openai.com/api/accounts/oauth/token',
          {
            method: 'POST',
            headers: { 'content-type': 'application/x-www-form-urlencoded' },
            body: new URLSearchParams({
              grant_type: 'refresh_token',
              client_id: credential.clientId,
              refresh_token: credential.refresh,
              resource: 'https://api.openai.com/v1',
            }),
          },
          async (response) => {
            if (response.status !== 200) throw loginRequired();
            return TokenResponse.parse(await response.json());
          },
        );
        credential = SubscriptionCredential.parse({
          ...credential,
          access: token.access_token,
          refresh: token.refresh_token,
          expires: Date.now() + token.expires_in * 1000 - Math.min(180_000, token.expires_in * 100),
          scopes: token.scope?.trim().split(/\s+/) ?? credential.scopes,
          ...(token.id_token ? { idToken: token.id_token } : {}),
        });
        session.encrypted = await this.#encrypt(credential);
        session.expires = credential.expires;
        session.refreshing = false;
        await this.ctx.storage.put('session', session);
        return credential.access;
      } catch {
        session.requiresLogin = true;
        await this.ctx.storage.put('session', session);
        throw loginRequired();
      }
    });
  }
  disconnect() {
    return this.#serialize(async () => {
      const session = await this.ctx.storage.get<Session>('session');
      let remoteRevocation = false;
      if (session) {
        session.requiresLogin = true;
        await this.ctx.storage.put('session', session);
        try {
          remoteRevocation = await revokeSubscription(await this.#decrypt(session));
        } catch {
          remoteRevocation = false;
        }
      }
      // Keep host identity and import tombstones after removing the renewable session.
      await this.ctx.storage.delete('session');
      return { ...(await this.#readStatus()), remoteRevocation };
    });
  }
}
