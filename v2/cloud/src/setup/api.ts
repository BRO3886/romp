import { SubscriptionStatus, OpenAIAuth } from '../subscription-schema.js';
import { createHmac } from 'node:crypto';
import { z } from 'zod';
import type { Profile } from './profile.js';
import { jsonRequest, type Fetch } from './http.js';
export type { Fetch } from './http.js';
export async function cloudflarePreflight(
  profile: Pick<Profile, 'name' | 'accountId'>,
  token: string,
  fetcher: Fetch,
) {
  if (!token)
    throw new Error(
      'Set CLOUDFLARE_API_TOKEN with Workers Scripts Edit and Workers Containers Write permissions',
    );
  const headers = { authorization: `Bearer ${token}` };
  const base = `https://api.cloudflare.com/client/v4/accounts/${profile.accountId}`;
  const envelope = z.object({ success: z.literal(true), result: z.unknown() });
  envelope.parse(await jsonRequest(fetcher, `${base}/containers/applications`, { headers }));
  const subdomain = z
    .object({ subdomain: z.string().regex(/^[a-z0-9-]+$/) })
    .parse(
      envelope.parse(await jsonRequest(fetcher, `${base}/workers/subdomain`, { headers })).result,
    );
  return `https://${profile.name}.${subdomain.subdomain}.workers.dev`;
}
export async function workerRequest(
  profile: Profile,
  fetcher: Fetch,
  path: string,
  method = 'GET',
  body?: unknown,
) {
  if (!profile.endpoint)
    throw new Error('Profile has no deployment endpoint. Run cloud setup first');
  return jsonRequest(fetcher, `${profile.endpoint}${path}`, {
    method,
    headers: {
      authorization: `Bearer ${profile.secrets.ADMIN_TOKEN}`,
      'content-type': 'application/json',
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
}
export async function statusRequest(
  profile: Profile,
  fetcher: Fetch,
  path: '/status' | '/concurrency' | '/pause' | '/resume',
  concurrency?: number,
) {
  const result = z
    .object({
      concurrency: z.number().int().min(1).max(32),
      paused: z.boolean(),
      subscription: SubscriptionStatus.optional(),
      runs: z.array(
        z.object({
          id: z.string(),
          state: z.enum(['queued', 'active', 'finished']),
          error: z.string(),
        }),
      ),
    })
    .parse(
      await workerRequest(
        profile,
        fetcher,
        path,
        path === '/status' ? 'GET' : path === '/concurrency' ? 'PUT' : 'POST',
        concurrency === undefined ? undefined : { concurrency },
      ),
    );
  if (concurrency !== undefined && result.concurrency !== concurrency)
    throw new Error('Worker did not apply the requested concurrency');
  return result;
}
export async function checkWorker(profile: Profile, fetcher: Fetch) {
  const doctor = z
    .object({
      ready: z.literal(true),
      openaiAuth: OpenAIAuth.default('api-key'),
      repositories: z.array(z.string()),
      concurrency: z.number(),
    })
    .parse(await workerRequest(profile, fetcher, '/doctor'));
  if (
    JSON.stringify(doctor.repositories) !== JSON.stringify(profile.repositories.map((r) => r.repo))
  )
    throw new Error('Deployed configuration differs from the local profile');
  if (doctor.openaiAuth !== profile.openaiAuth)
    throw new Error('Deployed OpenAI authentication mode differs from the local profile');
  const body = '{}';
  const response = await fetcher(`${profile.endpoint}/webhook`, {
    method: 'POST',
    redirect: 'error',
    signal: AbortSignal.timeout(30_000),
    body,
    headers: {
      'x-github-event': 'ping',
      'x-hub-signature-256': `sha256=${createHmac('sha256', profile.secrets.GITHUB_WEBHOOK_SECRET).update(body).digest('hex')}`,
    },
  });
  if (!response.ok || (await response.text()) !== 'pong')
    throw new Error('Deployed webhook secret failed its signed ping');
  return doctor;
}
