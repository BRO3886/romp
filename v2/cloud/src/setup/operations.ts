import type { authenticate } from '../../../src/adapters/node/login.js';
import { dirname } from 'node:path';
import { mkdir, rm } from 'node:fs/promises';
import { preflight } from '../config.js';
import { checkModels, checkModelSelection } from '../models.js';
import { checkWorker, cloudflarePreflight, statusRequest, type Fetch } from './api.js';
import { deploy, type Runner } from './deployment.js';
import { connectWebhook, ensureLabels, githubPreflight, verifyWebhook } from './github.js';
import { resolveSetup, type SetupOptions } from './resolution.js';
import {
  subscriptionDirectory,
  subscriptionPreflight,
  transferSubscription,
} from './subscription.js';
import {
  environment,
  loadProfile,
  privateDirectory,
  Profile,
  profilePath,
  saveProfile,
} from './profile.js';

export type Dependencies = {
  home: string;
  cwd: string;
  root: string;
  env: NodeJS.ProcessEnv;
  fetch: Fetch;
  runner: Runner;
  output: (value: unknown) => void;
  login: typeof authenticate;
};
export async function savedProfile(name: string, home: string) {
  const file = profilePath(home, name);
  const previous = await loadProfile(file);
  if (!previous)
    throw new Error(
      `No cloud deployment profile at ${file}. Run romp cloud setup --help for first-time setup. If you used another profile, select it with --profile NAME. Model login alone does not create a deployment profile.`,
    );
  return { file, profile: previous };
}
export async function setup(
  name: string,
  values: SetupOptions & { dryRun?: boolean; replaceWebhook?: boolean },
  d: Dependencies,
) {
  const file = profilePath(d.home, name);
  const previous = await loadProfile(file);
  const directory = subscriptionDirectory(file);
  const draft = await resolveSetup(values, previous, {
    ...d,
    subscriptionPreflight: () => subscriptionPreflight(directory, previous, d.fetch),
  });
  draft.endpoint = await cloudflarePreflight(draft, d.env['CLOUDFLARE_API_TOKEN'] ?? '', d.fetch);
  const github = await githubPreflight(draft, d.fetch, values.replaceWebhook ?? false);
  const profile = Profile.parse({ ...draft, repositories: github.repositories });
  d.output({
    account: profile.accountId,
    worker: profile.name,
    webhook: `${profile.endpoint}/webhook`,
    configFile: profile.configFile,
    repositories: profile.repositories.map((r) => ({
      repo: r.repo,
      installationId: r.installationId,
    })),
    concurrency: profile.concurrency,
    openaiAuth: profile.openaiAuth,
    review: false,
    replacesWebhook: github.hook.url || null,
    provisions: !values.dryRun,
  });
  await deploy(d.root, profile, d.env, true, d.runner);
  if (values.dryRun) return;
  await withProfileLock(
    file,
    async () => {
      await saveProfile(file, profile);
      await deploy(d.root, profile, d.env, false, d.runner);
      if (profile.openaiAuth === 'subscription')
        await transferSubscription(profile, directory, d.fetch);
      await checkWorker(profile, d.fetch);
      await statusRequest(profile, d.fetch, '/concurrency', profile.concurrency);
      await ensureLabels(profile, d.fetch);
      await connectWebhook(profile, d.fetch);
      d.output({ ready: true, endpoint: profile.endpoint, profile: name });
    },
    previous,
  );
}
export async function doctor(profile: Profile, d: Dependencies) {
  preflight(environment(profile));
  checkModelSelection(profile);
  await checkModels(
    {
      repositories: profile.repositories.filter(
        (r) => profile.openaiAuth !== 'subscription' || r.config.model.provider !== 'openai',
      ),
    },
    profile.secrets,
  );
  const endpoint = await cloudflarePreflight(profile, d.env['CLOUDFLARE_API_TOKEN'] ?? '', d.fetch);
  if (endpoint !== profile.endpoint)
    throw new Error('Cloudflare subdomain differs from the profile. Run cloud setup');
  const github = await githubPreflight(profile, d.fetch, false);
  if (JSON.stringify(github.repositories) !== JSON.stringify(profile.repositories))
    throw new Error('GitHub installation changed. Run cloud setup');
  await verifyWebhook(profile, d.fetch);
  const doctor = await checkWorker(profile, d.fetch);
  const status = await statusRequest(profile, d.fetch, '/status');
  if (status.concurrency !== profile.concurrency)
    throw new Error(
      'Remote concurrency differs from the profile. Use cloud configure to reconcile it',
    );
  d.output({ ...doctor, concurrency: status.concurrency, paused: status.paused });
}
export async function withProfileLock(
  file: string,
  operation: () => Promise<void>,
  previous: Profile | undefined,
) {
  await privateDirectory(dirname(file));
  const lock = `${file}.lock`;
  try {
    await mkdir(lock, { mode: 0o700 });
  } catch (error) {
    throw new Error(`Profile is locked. If no setup/configure command is running, remove ${lock}`, {
      cause: error,
    });
  }
  try {
    if (JSON.stringify(await loadProfile(file)) !== JSON.stringify(previous))
      throw new Error('Profile changed during preflight. Rerun the command');
    await operation();
  } finally {
    await rm(lock, { recursive: true, force: true });
  }
}
