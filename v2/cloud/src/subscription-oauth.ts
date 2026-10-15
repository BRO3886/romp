import { z } from 'zod';
import type { SubscriptionCredential } from './subscription-schema.js';

export async function oauthRequest<T>(
  url: string,
  init: RequestInit,
  consume: (response: Response) => Promise<T>,
  timeout = 30_000,
  fetcher: typeof fetch = fetch,
) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeout);
  try {
    return await consume(
      await fetcher(url, { ...init, redirect: 'manual', signal: controller.signal }),
    );
  } finally {
    clearTimeout(timer);
  }
}

export async function revokeSubscription(
  credential: SubscriptionCredential,
  fetcher: typeof fetch = fetch,
) {
  try {
    const { revocation_endpoint: endpoint } = await oauthRequest(
      'https://auth.openai.com/.well-known/openid-configuration',
      {},
      async (response) => {
        if (!response.ok) throw new Error('Discovery failed');
        return z.object({ revocation_endpoint: z.url() }).parse(await response.json());
      },
      15_000,
      fetcher,
    );
    if (new URL(endpoint).origin !== 'https://auth.openai.com')
      throw new Error('Invalid revocation endpoint');
    return await oauthRequest(
      endpoint,
      {
        method: 'POST',
        headers: { 'content-type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({
          token: credential.refresh,
          token_type_hint: 'refresh_token',
          client_id: credential.clientId,
        }),
      },
      (response) => Promise.resolve(response.status === 200),
      15_000,
      fetcher,
    );
  } catch {
    return false;
  }
}
