import { mkdir, rm } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { createRequire } from 'node:module';
import { preflight } from '../config.js';
import { checkModels } from '../models.js';
import { checkWorker, cloudflarePreflight, statusRequest, type Fetch } from './api.js';
import { deploy, runWrangler, type Runner } from './deployment.js';
import { connectWebhook, ensureLabels, githubPreflight, verifyWebhook } from './github.js';
import { resolveSetup } from './resolution.js';
import {
  environment,
  loadProfile,
  privateDirectory,
  Profile,
  profilePath,
  saveProfile,
} from './profile.js';

const help = `romp-v2 cloud
  setup --account ID --name WORKER --app-id ID --private-key FILE --repo OWNER/REPO
        [--config romp-v2.toml] [--dockerfile FILE] [--concurrency 1..32]
        [--dry-run] [--replace-webhook] [--profile NAME]
  doctor | status | pause | resume [--profile NAME]
  configure --concurrency 1..32 [--profile NAME]

First setup requires an installed GitHub App and CLOUDFLARE_API_TOKEN plus your
model API key in the environment. Reruns reuse the private local profile.
--dry-run validates and bundles without deploying or changing the webhook.
Pause keeps accepting issues and drains active runs without starting queued ones.
`;
type Dependencies = {
  home: string;
  cwd: string;
  root: string;
  env: NodeJS.ProcessEnv;
  fetch: Fetch;
  runner: Runner;
  output: (value: unknown) => void;
};
export async function main(args: string[], dependencies: Partial<Dependencies> = {}) {
  const d: Dependencies = {
    home: homedir(),
    cwd: process.cwd(),
    root: dirname(createRequire(import.meta.url).resolve('@romp/cloud/package.json')),
    env: process.env,
    fetch,
    runner: runWrangler,
    output: (value) =>
      console.log(typeof value === 'string' ? value : JSON.stringify(value, null, 2)),
    ...dependencies,
  };
  const { positionals, values } = parseArgs({
    args,
    allowPositionals: true,
    options: {
      help: { type: 'boolean' },
      profile: { type: 'string', default: 'default' },
      account: { type: 'string' },
      name: { type: 'string' },
      'app-id': { type: 'string' },
      'private-key': { type: 'string' },
      repo: { type: 'string', multiple: true },
      config: { type: 'string' },
      dockerfile: { type: 'string' },
      concurrency: { type: 'string' },
      'dry-run': { type: 'boolean' },
      'replace-webhook': { type: 'boolean' },
    },
  });
  const [action, ...extra] = positionals;
  if (values.help || !action) {
    d.output(help);
    return;
  }
  if (
    extra.length ||
    !['setup', 'doctor', 'status', 'configure', 'pause', 'resume'].includes(action)
  )
    throw new Error('Unknown cloud command. Use cloud --help');
  const allowed =
    action === 'setup'
      ? Object.keys(values)
      : action === 'configure'
        ? ['profile', 'concurrency']
        : ['profile'];
  if (Object.keys(values).some((key) => !allowed.includes(key)))
    throw new Error(`Unsupported option for cloud ${action}`);
  const file = profilePath(d.home, values.profile);
  const previous = await loadProfile(file);
  if (action === 'setup') {
    const draft = await resolveSetup(values, previous, d);
    draft.endpoint = await cloudflarePreflight(draft, d.env['CLOUDFLARE_API_TOKEN'] ?? '', d.fetch);
    const github = await githubPreflight(draft, d.fetch, values['replace-webhook'] ?? false);
    const profile = Profile.parse({ ...draft, repositories: github.repositories });
    d.output({
      account: profile.accountId,
      worker: profile.name,
      webhook: `${profile.endpoint}/webhook`,
      repositories: profile.repositories.map((r) => ({
        repo: r.repo,
        installationId: r.installationId,
      })),
      concurrency: profile.concurrency,
      review: false,
      replacesWebhook: github.hook.url || null,
      provisions: !values['dry-run'],
    });
    await deploy(d.root, profile, d.env, true, d.runner);
    if (values['dry-run']) return;
    await withProfileLock(
      file,
      async () => {
        await saveProfile(file, profile);
        await deploy(d.root, profile, d.env, false, d.runner);
        await checkWorker(profile, d.fetch);
        await statusRequest(profile, d.fetch, '/concurrency', profile.concurrency);
        await ensureLabels(profile, d.fetch);
        await connectWebhook(profile, d.fetch);
        d.output({ ready: true, endpoint: profile.endpoint, profile: values.profile });
      },
      previous,
    );
    return;
  }
  if (!previous) throw new Error('No cloud profile. Run cloud setup first');
  if (action === 'doctor') {
    preflight(environment(previous));
    await checkModels(previous, previous.secrets);
    const endpoint = await cloudflarePreflight(
      previous,
      d.env['CLOUDFLARE_API_TOKEN'] ?? '',
      d.fetch,
    );
    if (endpoint !== previous.endpoint)
      throw new Error('Cloudflare subdomain differs from the profile. Run cloud setup');
    const github = await githubPreflight(previous, d.fetch, false);
    if (JSON.stringify(github.repositories) !== JSON.stringify(previous.repositories))
      throw new Error('GitHub installation changed. Run cloud setup');
    await verifyWebhook(previous, d.fetch);
    const doctor = await checkWorker(previous, d.fetch);
    const status = await statusRequest(previous, d.fetch, '/status');
    if (status.concurrency !== previous.concurrency)
      throw new Error(
        'Remote concurrency differs from the profile. Use cloud configure to reconcile it',
      );
    d.output({ ...doctor, concurrency: status.concurrency, paused: status.paused });
    return;
  }
  if (action === 'configure') {
    const concurrency = Number(values.concurrency);
    if (!Number.isSafeInteger(concurrency) || concurrency < 1 || concurrency > 32)
      throw new Error('--concurrency must be an integer from 1 to 32');
    await withProfileLock(
      file,
      async () => {
        d.output(await statusRequest(previous, d.fetch, '/concurrency', concurrency));
        previous.concurrency = concurrency;
        await saveProfile(file, previous);
      },
      previous,
    );
    return;
  }
  if (action !== 'status' && action !== 'pause' && action !== 'resume')
    throw new Error('Unknown cloud command');
  d.output(await statusRequest(previous, d.fetch, `/${action}`));
}
async function withProfileLock(
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
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : 'Cloud command failed');
    process.exitCode = 1;
  });
}
