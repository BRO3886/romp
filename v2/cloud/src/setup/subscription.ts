import { revokeSubscription } from '../subscription-oauth.js';
import { z } from 'zod';
import { randomUUID } from 'node:crypto';
import { readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { authenticate } from '../../../src/adapters/node/login.js';
import { FileCredentials } from '../../../src/adapters/node/auth.js';
import { SubscriptionImport, SubscriptionStatus } from '../subscription-schema.js';
import { privateDirectory, readPrivateJson, savePrivateJson, type Profile } from './profile.js';
import { checkWorker, workerRequest, type Fetch } from './api.js';

export function subscriptionDirectory(profileFile: string) {
  return `${profileFile}.auth`;
}
export async function pendingSubscription(directory: string) {
  const value = await readPrivateJson(join(directory, 'transfer.json'));
  return value === undefined ? undefined : SubscriptionImport.parse(value);
}
export async function cloudLogin(directory: string, login: typeof authenticate = authenticate) {
  await privateDirectory(directory);
  await login('login', 'openai', directory, 'Romp Cloud');
  const credential = await new FileCredentials(directory).read('openai');
  const pending = SubscriptionImport.parse({
    sessionId: randomUUID(),
    hostId: (await readFile(join(directory, 'device-id'), 'utf8')).trim(),
    credential,
  });
  await savePrivateJson(join(directory, 'transfer.json'), pending);
  await new FileCredentials(directory).delete('openai');
  return pending;
}
export async function transferSubscription(profile: Profile, directory: string, fetcher: Fetch) {
  const pending = await pendingSubscription(directory);
  if (!pending) return;
  const status = SubscriptionStatus.parse(
    await workerRequest(profile, fetcher, '/subscription', 'PUT', pending),
  );
  if (!status.configured || status.requiresLogin || status.sessionId !== pending.sessionId)
    throw new Error('Cloud subscription needs login. Run romp cloud login');
  // Remove the renewable local copy only after the Worker acknowledges this exact session.
  await new FileCredentials(directory).delete('openai');
  await rm(join(directory, 'transfer.json'));
}
export async function subscriptionPreflight(
  directory: string,
  previous: Profile | undefined,
  fetcher: Fetch,
) {
  const pending = await pendingSubscription(directory);
  if (pending) {
    if (pending.credential.expires <= Date.now() + 60_000)
      throw new Error('Cloud login expired before transfer. Run romp cloud login');
    return;
  }
  if (previous?.openaiAuth === 'subscription' && previous.endpoint) {
    await checkWorker(previous, fetcher);
    return;
  }
  throw new Error('Run romp cloud login before setup with --openai-auth subscription');
}

export async function cloudLogout(profile: Profile | undefined, directory: string, fetcher: Fetch) {
  const pending = await pendingSubscription(directory);
  const result = profile?.endpoint
    ? z
        .object({ remoteRevocation: z.boolean() })
        .passthrough()
        .parse(await workerRequest(profile, fetcher, '/subscription', 'DELETE'))
    : { remoteRevocation: true };
  if (pending)
    result.remoteRevocation =
      (await revokeSubscription(pending.credential, fetcher)) && result.remoteRevocation;
  await rm(join(directory, 'transfer.json'), { force: true });
  await new FileCredentials(directory).delete('openai');
  return result;
}
