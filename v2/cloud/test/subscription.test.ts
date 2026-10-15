import { z } from 'zod';
import { env } from 'cloudflare:workers';
import {
  evictDurableObject,
  reset,
  runInDurableObject,
  runDurableObjectAlarm,
} from 'cloudflare:test';
import { afterAll, afterEach, beforeAll, beforeEach, expect, test, vi } from 'vitest';
import { setupNetwork } from '@msw/cloudflare';
import { http, HttpResponse, passthrough } from 'msw';
import { validateAuthentication } from '../src/config.js';
import { cloudModels } from '../src/models.js';
import worker from '../src/worker.js';
import { config, submission } from './fixture.js';
import { IssueRun } from './worker.js';
import { completedMessage, responsesStream } from './responses.js';

const network = setupNetwork();
beforeAll(() => network.enable());
afterAll(() => network.disable());
beforeEach(() =>
  network.use(
    http.all(/.*/, ({ request }) => {
      if (new URL(request.url).hostname === '127.0.0.1') return passthrough();
      throw new Error(`Unexpected network request to ${new URL(request.url).origin}`);
    }),
  ),
);

afterEach(async () => {
  network.resetHandlers();
  vi.restoreAllMocks();
  await reset();
});
const input = () => ({
  sessionId: crypto.randomUUID(),
  hostId: 'd68a10a3-8073-4f45-bf8c-66bbb548e095',
  credential: {
    type: 'oauth' as const,
    access: 'subscription-access',
    refresh: 'subscription-refresh',
    expires: Date.now() + 3_600_000,
    clientId: 'issued-client',
    scopes: ['openid', 'offline_access', 'resource.invoke', 'chatgpt.tokens.use.direct'],
  },
});
const token = (access = 'rotated-access', refresh = 'rotated-refresh') => ({
  access_token: access,
  refresh_token: refresh,
  token_type: 'Bearer',
  expires_in: 3600,
});

test('subscription configuration requires an encryption key instead of an OpenAI API key', () => {
  const credentials = {
    GITHUB_APP_ID: '123',
    GITHUB_PRIVATE_KEY: 'private-key',
    GITHUB_WEBHOOK_SECRET: 'w'.repeat(32),
    ADMIN_TOKEN: 'a'.repeat(32),
    OPENAI_AUTH: 'subscription',
    SUBSCRIPTION_KEY: '11'.repeat(32),
  };
  expect(() => validateAuthentication({ repositories: [config] }, credentials)).not.toThrow();
  expect(() =>
    validateAuthentication({ repositories: [config] }, { ...credentials, SUBSCRIPTION_KEY: '' }),
  ).toThrow(/SUBSCRIPTION_KEY/);
});

test('concurrent model authentication refreshes once and preserves rotation across actor eviction and upload replay', async () => {
  const auth = env.AUTH.getByName('openai');
  const original = input();
  original.credential.expires = Date.now() + 30_000;
  await auth.importSession(original);
  let refreshes = 0;
  network.use(
    http.post('https://auth.openai.com/api/accounts/oauth/token', async ({ request }) => {
      const body = new URLSearchParams(new TextDecoder().decode(await request.arrayBuffer()));
      expect(Object.fromEntries(body)).toEqual({
        grant_type: 'refresh_token',
        client_id: 'issued-client',
        refresh_token: refreshes ? 'rotated-refresh' : 'subscription-refresh',
        resource: 'https://api.openai.com/v1',
      });
      refreshes++;
      return HttpResponse.json(
        token(
          refreshes === 1 ? 'rotated-access' : 'second-access',
          refreshes === 1 ? 'rotated-refresh' : 'second-refresh',
        ),
      );
    }),
  );
  const models = Array.from({ length: 8 }, () =>
    cloudModels({ OPENAI_AUTH: 'subscription', OPENAI_API_KEY: 'sk-never-fallback' }, () =>
      auth.access(),
    ),
  );
  const results = await Promise.all(
    models.map(async (models) => {
      const selected = models.getModel('openai', 'gpt-5.4');
      if (!selected) throw new Error('Missing model');
      return models.getAuth(selected);
    }),
  );
  expect(results.every((result) => result?.auth.apiKey === 'rotated-access')).toBe(true);
  expect(refreshes).toBe(1);
  await evictDurableObject(auth);
  await auth.importSession(original);
  expect(await auth.access()).toBe('rotated-access');
  await runInDurableObject(auth, async (_instance, ctx) => {
    const stored = await ctx.storage.list();
    const serialized = JSON.stringify([...stored]);
    for (const secret of [
      'subscription-access',
      'subscription-refresh',
      'rotated-access',
      'rotated-refresh',
      'issued-client',
    ])
      expect(serialized).not.toContain(secret);
    const state = await ctx.storage.get<{ expires: number }>('session');
    if (!state) throw new Error('Missing session');
    expect(state.expires).toBeGreaterThan(Date.now());
  });
  vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 3_600_000);
  expect(await auth.access()).toBe('second-access');
  expect(refreshes).toBe(2);
});

test('interrupted and rejected refreshes fail closed and never fall back to an API key', async () => {
  for (const failure of ['interrupted', 'rejected', 'redirected']) {
    const auth = env.AUTH.getByName(failure);
    const original = input();
    original.credential.expires = Date.now() + 30_000;
    await auth.importSession(original);
    if (failure === 'interrupted') {
      await runInDurableObject(auth, async (_instance, ctx) => {
        const state = await ctx.storage.get<{ refreshing: boolean }>('session');
        if (!state) throw new Error('Missing session');
        state.refreshing = true;
        await ctx.storage.put('session', state);
      });
      await evictDurableObject(auth);
    } else
      network.use(
        http.post('https://auth.openai.com/api/accounts/oauth/token', () =>
          HttpResponse.json(
            { error: 'invalid_grant', secret: original.credential.refresh },
            {
              status: failure === 'redirected' ? 302 : 400,
              headers:
                failure === 'redirected' ? { location: 'https://untrusted.example/token' } : {},
            },
          ),
        ),
      );
    const models = cloudModels(
      { OPENAI_AUTH: 'subscription', OPENAI_API_KEY: 'sk-never-fallback' },
      () => auth.access(),
    );
    const selected = models.getModel('openai', 'gpt-5.4');
    if (!selected) throw new Error('Missing model');
    await expect(models.getAuth(selected)).rejects.toThrow(/cloud login/);
    await expect(Promise.resolve(auth.access())).rejects.toThrow(/cloud login/);
    expect(await auth.status()).toMatchObject({ requiresLogin: true });
    const replacement = input();
    await auth.importSession(replacement);
    expect(await auth.access()).toBe('subscription-access');
    await expect(Promise.resolve(auth.importSession(original))).rejects.toThrow(/replaced/);
  }
});

test('subscription administration rejects unauthorized, malformed, and changed-host imports without exposing tokens', async () => {
  const pair = await crypto.subtle.generateKey(
    {
      name: 'RSASSA-PKCS1-v1_5',
      modulusLength: 2048,
      publicExponent: new Uint8Array([1, 0, 1]),
      hash: 'SHA-256',
    },
    true,
    ['sign', 'verify'],
  );
  if (!('privateKey' in pair)) throw new Error('Expected signing key pair');
  const exported = await crypto.subtle.exportKey('pkcs8', pair.privateKey);
  if (!(exported instanceof ArrayBuffer)) throw new Error('Expected PKCS8 bytes');
  const bytes = new Uint8Array(exported);
  const bindings = {
    ...env,
    OPENAI_AUTH: 'subscription',
    GITHUB_PRIVATE_KEY: `-----BEGIN PRIVATE KEY-----\n${btoa(String.fromCharCode(...bytes))}\n-----END PRIVATE KEY-----`,
  };
  const original = input();
  const request = (body: unknown, authorized = true) =>
    new Request('https://romp/subscription', {
      method: 'PUT',
      headers: authorized ? { authorization: `Bearer ${env.ADMIN_TOKEN}` } : {},
      body: JSON.stringify(body),
    });
  expect((await worker.fetch(request(original, false), bindings)).status).toBe(401);
  expect(
    (
      await worker.fetch(
        request({
          ...original,
          credential: { ...original.credential, clientId: 'dynamic_agent_client' },
        }),
        bindings,
      )
    ).status,
  ).toBe(400);
  expect(
    (
      await worker.fetch(
        request({ ...original, credential: { ...original.credential, scopes: ['openid'] } }),
        bindings,
      )
    ).status,
  ).toBe(400);
  expect((await worker.fetch(request(original), bindings)).status).toBe(200);
  const doctor = await worker.fetch(
    new Request('https://romp/doctor', { headers: { authorization: `Bearer ${env.ADMIN_TOKEN}` } }),
    bindings,
  );
  expect(doctor.status).toBe(200);
  expect(await doctor.json()).toMatchObject({ ready: true, openaiAuth: 'subscription' });
  const rejected = await worker.fetch(
    request({ ...input(), hostId: crypto.randomUUID() }),
    bindings,
  );
  expect(rejected.status).toBe(409);
  expect(await rejected.text()).not.toContain(original.credential.refresh);
  const response = await worker.fetch(
    new Request('https://romp/subscription', {
      headers: { authorization: `Bearer ${env.ADMIN_TOKEN}` },
    }),
    bindings,
  );
  expect(await response.json()).toMatchObject({
    configured: true,
    requiresLogin: false,
    hostId: original.hostId,
  });
});

test('the real OpenAI adapter sends subscription-compatible Responses requests and Pi publishes from its streamed tool calls', async () => {
  const auth = env.AUTH.getByName('openai');
  const original = input();
  original.credential.expires = Date.now() + 30_000;
  await auth.importSession(original);
  let refreshes = 0;
  let requests = 0;
  network.use(
    http.post('https://auth.openai.com/api/accounts/oauth/token', () => {
      refreshes++;
      return HttpResponse.json(token());
    }),
    http.post('https://api.openai.com/v1/responses', async ({ request }) => {
      expect(request.headers.get('authorization')).toBe('Bearer rotated-access');
      const body: unknown = await request.json();
      expect(body).toMatchObject({ store: false, stream: true });
      const inputs = z
        .object({ input: z.array(z.object({ role: z.string().optional() }).passthrough()) })
        .parse(body).input;
      expect(inputs.some((item) => item.role === 'system')).toBe(false);
      for (const field of [
        'max_output_tokens',
        'temperature',
        'top_p',
        'background',
        'previous_response_id',
      ])
        expect(body).not.toHaveProperty(field);
      requests++;
      const output =
        requests === 1
          ? [
              {
                type: 'function_call',
                id: 'fc_answer',
                call_id: 'call_answer',
                name: 'write',
                arguments: JSON.stringify({ path: 'answer.txt', content: '42' }),
                status: 'completed',
              },
              {
                type: 'function_call',
                id: 'fc_pr',
                call_id: 'call_pr',
                name: 'write',
                arguments: JSON.stringify({
                  path: '.romp/pull-request.md',
                  content:
                    '---\ntitle: Change answer\ncommit: fix: change answer\n---\nSet answer.txt to 42.',
                }),
                status: 'completed',
              },
            ]
          : [completedMessage];
      return new HttpResponse(responsesStream(output), {
        headers: { 'content-type': 'text/event-stream' },
      });
    }),
  );
  const value = submission(71);
  const run = env.RUNS.getByName(value.id);
  await runInDurableObject(run, (instance) => {
    if (!(instance instanceof IssueRun)) throw new Error('Wrong test host');
    instance.useSubscription();
  });
  await run.start(value);
  await runDurableObjectAlarm(run);
  const deadline = Date.now() + 30_000;
  while ((await run.status()).state !== 'finished') {
    if (Date.now() > deadline) throw new Error('Delivery timed out');
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
  const status = await run.status();
  expect('job' in status && status.job?.outcome, JSON.stringify(status)).toBe('done');
  expect('job' in status && status.job?.prUrl).toBe('https://github.com/test/repo/pull/171');
  expect(refreshes).toBe(1);
  expect(requests).toBe(2);
  await runInDurableObject(run, async (_instance, ctx) => {
    const tables = [
      ...ctx.storage.sql.exec<{ name: string }>(
        "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT GLOB '_cf_*' AND name NOT GLOB 'sqlite_*'",
      ),
    ];
    const contents = tables.map(({ name }) => [
      ...ctx.storage.sql.exec(`SELECT * FROM "${name.replaceAll('"', '""')}"`),
    ]);
    const serialized = JSON.stringify([contents, [...(await ctx.storage.list())]]);
    for (const secret of [
      'subscription-refresh',
      'rotated-refresh',
      'subscription-access',
      'rotated-access',
    ])
      expect(serialized).not.toContain(secret);
  });
});

test('logout revokes the renewable session, clears encrypted credentials, and rejects old upload retries', async () => {
  for (const success of [true, false]) {
    const auth = env.AUTH.getByName(`logout-${success}`);
    const original = input();
    await auth.importSession(original);
    network.use(
      http.get('https://auth.openai.com/.well-known/openid-configuration', () =>
        HttpResponse.json({ revocation_endpoint: 'https://auth.openai.com/oauth/revoke' }),
      ),
      http.post('https://auth.openai.com/oauth/revoke', async ({ request }) => {
        expect(
          Object.fromEntries(
            new URLSearchParams(new TextDecoder().decode(await request.arrayBuffer())),
          ),
        ).toEqual({
          token: original.credential.refresh,
          token_type_hint: 'refresh_token',
          client_id: 'issued-client',
        });
        return new HttpResponse(null, { status: success ? 200 : 503 });
      }),
    );
    expect(await auth.disconnect()).toMatchObject({ configured: false, remoteRevocation: success });
    await evictDurableObject(auth);
    await expect(Promise.resolve(auth.access())).rejects.toThrow(/cloud login/);
    await expect(Promise.resolve(auth.importSession(original))).rejects.toThrow(/replaced/);
    await runInDurableObject(auth, async (_instance, ctx) => {
      expect(await ctx.storage.get('session')).toBeUndefined();
    });
  }
});
