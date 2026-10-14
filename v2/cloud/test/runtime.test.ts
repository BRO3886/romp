import { env } from 'cloudflare:workers';
import {
  runInDurableObject,
  evictDurableObject,
  runDurableObjectAlarm,
  reset,
} from 'cloudflare:test';
import { afterEach, beforeAll, expect, test } from 'vitest';
import worker from '../src/worker.js';
import { runId, type Bindings, type Submission } from '../src/config.js';
import { config } from './fixture.js';
afterEach(async () => {
  await reset();
});
const bindings: Bindings = {
  ...env,
  REPOSITORIES: JSON.stringify([config]),
  CONCURRENCY: '1',
  GITHUB_APP_ID: '123',
  GITHUB_PRIVATE_KEY: 'test-key',
  GITHUB_WEBHOOK_SECRET: 'w'.repeat(32),
  ADMIN_TOKEN: 'a'.repeat(32),
  OPENAI_API_KEY: 'test',
};
beforeAll(async () => {
  const key = await crypto.subtle.generateKey(
    {
      name: 'RSASSA-PKCS1-v1_5',
      modulusLength: 2048,
      publicExponent: new Uint8Array([1, 0, 1]),
      hash: 'SHA-256',
    },
    true,
    ['sign', 'verify'],
  );
  if (!('privateKey' in key)) throw new Error('Expected signing key pair');
  const exported = await crypto.subtle.exportKey('pkcs8', key.privateKey);
  if (!(exported instanceof ArrayBuffer)) throw new Error('Expected PKCS8 bytes');
  const bytes = new Uint8Array(exported);
  bindings.GITHUB_PRIVATE_KEY = `-----BEGIN PRIVATE KEY-----\n${btoa(String.fromCharCode(...bytes))}\n-----END PRIVATE KEY-----`;
});
export function submission(n = 17): Submission {
  return {
    id: runId(123, n),
    repositoryId: 123,
    repository: config,
    issue: {
      number: n,
      title: 'Change answer',
      body: 'Set answer.txt to 42',
      url: `https://github.com/test/repo/issues/${n}`,
      labels: [],
    },
  };
}
async function request(payload: unknown, event = 'issues') {
  const body = JSON.stringify(payload);
  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(bindings.GITHUB_WEBHOOK_SECRET),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const signature = new Uint8Array(
    await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(body)),
  );
  return new Request('https://romp/webhook', {
    method: 'POST',
    body,
    headers: {
      'x-github-event': event,
      'x-hub-signature-256': `sha256=${Array.from(signature, (v) => v.toString(16).padStart(2, '0')).join('')}`,
    },
  });
}
function payload(action = 'opened') {
  return {
    action,
    installation: { id: 456 },
    repository: { id: 123, full_name: 'test/repo' },
    issue: {
      number: 17,
      title: 'Change answer',
      body: null,
      html_url: 'https://github.com/test/repo/issues/17',
      labels: [],
    },
  };
}
test('signed issue events are durably admitted once across duplicate delivery and eviction', async () => {
  expect((await worker.fetch(await request(payload()), bindings)).status).toBe(202);
  const scheduler = env.SCHEDULER.getByName('installation');
  expect((await scheduler.status()).runs).toHaveLength(1);
  await evictDurableObject(scheduler);
  expect((await worker.fetch(await request(payload()), bindings)).status).toBe(202);
  expect((await scheduler.status()).runs).toHaveLength(1);
});
test('signature, installation, body size, and action gates reject before admission', async () => {
  expect(
    (
      await worker.fetch(
        new Request('https://romp/webhook', { method: 'POST', body: '{}' }),
        bindings,
      )
    ).status,
  ).toBe(401);
  expect((await worker.fetch(await request(payload('edited')), bindings)).status).toBe(204);
  expect(
    (await worker.fetch(await request({ ...payload(), installation: { id: 999 } }), bindings))
      .status,
  ).toBe(403);
  expect(
    (
      await worker.fetch(
        new Request('https://romp/webhook', { method: 'POST', body: 'x'.repeat(1_048_577) }),
        bindings,
      )
    ).status,
  ).toBe(413);
  expect((await env.SCHEDULER.getByName('installation').status()).runs).toHaveLength(0);
});
test('concurrency configuration survives eviction and lowering does not cancel active runs', async () => {
  const scheduler = env.SCHEDULER.getByName('installation');
  await scheduler.enqueue(submission());
  await scheduler.enqueue(submission(18));
  await runInDurableObject(scheduler, (_instance, ctx) => {
    ctx.storage.sql.exec("UPDATE queue SET state='active'");
  });
  await scheduler.configure(1);
  await evictDurableObject(scheduler);
  expect((await scheduler.status()).concurrency).toBe(1);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'active')).toHaveLength(2);
});
test('queued cancellation survives duplicate delivery and cannot restart the issue', async () => {
  const scheduler = env.SCHEDULER.getByName('installation');
  await scheduler.enqueue(submission());
  await scheduler.cancel(submission().id);
  await evictDurableObject(scheduler);
  await scheduler.enqueue(submission());
  expect((await scheduler.status()).runs).toEqual([
    { id: submission().id, state: 'finished', error: '' },
  ]);
});
test('doctor and administration require authentication and validate configuration without provisioning', async () => {
  expect((await worker.fetch(new Request('https://romp/status'), bindings)).status).toBe(401);
  expect(
    (
      await worker.fetch(
        new Request('https://romp/doctor', {
          headers: { authorization: `Bearer ${bindings.ADMIN_TOKEN}` },
        }),
        bindings,
      )
    ).status,
  ).toBe(200);
  expect(
    (
      await worker.fetch(
        new Request('https://romp/doctor', {
          headers: { authorization: `Bearer ${bindings.ADMIN_TOKEN}` },
        }),
        { ...bindings, REPOSITORIES: '[]' },
      )
    ).status,
  ).toBe(422);
});
test('pause survives eviction, keeps arrivals queued, and resume grants their slots', async () => {
  const scheduler = env.SCHEDULER.getByName('installation');
  const toggle = (path: string, authorized = true) =>
    worker.fetch(
      new Request(`https://romp/${path}`, {
        method: 'POST',
        headers: authorized ? { authorization: `Bearer ${bindings.ADMIN_TOKEN}` } : {},
      }),
      bindings,
    );
  expect((await toggle('pause', false)).status).toBe(401);
  expect((await toggle('pause')).status).toBe(200);
  await evictDurableObject(scheduler);
  await scheduler.enqueue(submission(51));
  await runDurableObjectAlarm(scheduler);
  expect(await scheduler.status()).toMatchObject({ paused: true, runs: [{ state: 'queued' }] });
  expect(
    await runInDurableObject(scheduler, (_instance, ctx) => ctx.storage.getAlarm()),
  ).toBeNull();
  expect((await toggle('resume')).status).toBe(200);
  await runDurableObjectAlarm(scheduler);
  expect(await scheduler.status()).toMatchObject({ paused: false, runs: [{ state: 'active' }] });
});
