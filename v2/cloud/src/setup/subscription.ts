import { revokeSubscription } from '../subscription-oauth.js';
import { z } from 'zod';
import { randomUUID } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { authenticate } from '../../../src/adapters/node/login.js';
import { createCredentials } from '../../../src/adapters/node/auth.js';
import { SecretStorage } from '../../../src/adapters/node/secret-storage.js';
import { SubscriptionImport, SubscriptionStatus } from '../subscription-schema.js';
import { privateDirectory, type Profile } from './profile.js';
import { checkWorker, workerRequest, type Fetch } from './api.js';

export function subscriptionDirectory(profileFile: string) {
  return `${profileFile}.auth`;
}
export async function pendingSubscription(directory: string) {
  const value = await new SecretStorage(join(directory, 'transfer.json')).read();
  return value === undefined ? undefined : SubscriptionImport.parse(value);
}
export async function cloudLogin(directory: string, login: typeof authenticate = authenticate) {
  await privateDirectory(directory);
  await login('login', 'openai', directory, 'Romp Cloud');
  const credential = await createCredentials(directory).read('openai');
  const pending = SubscriptionImport.parse({
    sessionId: randomUUID(),
    hostId: (await readFile(join(directory, 'device-id'), 'utf8')).trim(),
    credential,
  });
  await new SecretStorage(join(directory, 'transfer.json')).write(pending);
  await createCredentials(directory).delete('openai');
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
  await createCredentials(directory).delete('openai');
  await new SecretStorage(join(directory, 'transfer.json')).delete();
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
  await new SecretStorage(join(directory, 'transfer.json')).delete();
  await createCredentials(directory).delete('openai');
  return result;
}
