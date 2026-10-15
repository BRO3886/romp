import { randomBytes } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { parseConfig } from '../../../src/domain/config.js';
import { validateAuthentication } from '../config.js';
import { checkModels, checkModelSelection } from '../models.js';
import { appJwt } from '../github.js';
import { SetupDraft, type Profile } from './profile.js';

export type SetupOptions = {
  'openai-auth'?: string | undefined;
  account?: string | undefined;
  name?: string | undefined;
  'app-id'?: string | undefined;
  'private-key'?: string | undefined;
  repo?: string[] | undefined;
  config?: string | undefined;
  dockerfile?: string | undefined;
  concurrency?: string | undefined;
};
export async function resolveSetup(
  values: SetupOptions,
  previous: Profile | undefined,
  context: {
    cwd: string;
    root: string;
    env: NodeJS.ProcessEnv;
    subscriptionPreflight?: () => Promise<void>;
  },
) {
  if (
    previous &&
    ((values.account && values.account !== previous.accountId) ||
      (values.name && values.name !== previous.name) ||
      (values['app-id'] && values['app-id'] !== previous.appId))
  )
    throw new Error(
      'Account, Worker name, and App ID are fixed for a profile. Select a new --profile',
    );
  const configFile = resolve(context.cwd, values.config ?? previous?.configFile ?? 'romp-v2.toml');
  const config = parseConfig(await readFile(configFile, 'utf8'));
  const repos = values.repo ?? previous?.repositories.map((r) => r.repo);
  if (!repos?.length)
    throw new Error('First setup requires --repo OWNER/REPO (repeat for multiple repositories)');
  const mode = values['openai-auth'] ?? previous?.openaiAuth ?? 'api-key';
  const draft = SetupDraft.parse({
    openaiAuth: mode,
    accountId: values.account ?? previous?.accountId,
    name: values.name ?? previous?.name,
    appId: values['app-id'] ?? previous?.appId,
    configFile,
    dockerfile: resolve(
      context.cwd,
      values.dockerfile ?? previous?.dockerfile ?? resolve(context.root, 'Dockerfile'),
    ),
    concurrency:
      values.concurrency === undefined ? (previous?.concurrency ?? 1) : Number(values.concurrency),
    repositories: repos.map((repo) => ({
      repo,
      config: { ...config, review: false },
    })),
    ...(previous?.endpoint ? { endpoint: previous.endpoint } : {}),
    secrets: {
      GITHUB_PRIVATE_KEY: values['private-key']
        ? await readFile(resolve(context.cwd, values['private-key']), 'utf8')
        : (context.env['GITHUB_PRIVATE_KEY'] ?? previous?.secrets.GITHUB_PRIVATE_KEY),
      GITHUB_WEBHOOK_SECRET:
        previous?.secrets.GITHUB_WEBHOOK_SECRET ?? randomBytes(32).toString('hex'),
      ADMIN_TOKEN: previous?.secrets.ADMIN_TOKEN ?? randomBytes(32).toString('hex'),
      ...(mode === 'subscription' || previous?.secrets.SUBSCRIPTION_KEY
        ? {
            SUBSCRIPTION_KEY: previous?.secrets.SUBSCRIPTION_KEY ?? randomBytes(32).toString('hex'),
          }
        : {}),
      ...(mode !== 'subscription' &&
      (context.env['OPENAI_API_KEY'] ?? previous?.secrets.OPENAI_API_KEY)
        ? { OPENAI_API_KEY: context.env['OPENAI_API_KEY'] ?? previous?.secrets.OPENAI_API_KEY }
        : {}),
      ...((context.env['ANTHROPIC_API_KEY'] ?? previous?.secrets.ANTHROPIC_API_KEY)
        ? {
            ANTHROPIC_API_KEY:
              context.env['ANTHROPIC_API_KEY'] ?? previous?.secrets.ANTHROPIC_API_KEY,
          }
        : {}),
    },
  });
  await readFile(draft.dockerfile);
  validateAuthentication(draft, {
    GITHUB_APP_ID: draft.appId,
    OPENAI_AUTH: draft.openaiAuth,
    ...draft.secrets,
  });
  await appJwt(draft.appId, draft.secrets.GITHUB_PRIVATE_KEY);
  checkModelSelection(draft);
  if (
    draft.openaiAuth === 'subscription' &&
    draft.repositories.some((r) => r.config.model.provider === 'openai')
  ) {
    if (!context.subscriptionPreflight)
      throw new Error('Cloud subscription preflight is unavailable');
    await context.subscriptionPreflight();
  }
  await checkModels(
    {
      repositories: draft.repositories.filter(
        (r) => draft.openaiAuth !== 'subscription' || r.config.model.provider !== 'openai',
      ),
    },
    draft.secrets,
  );
  return draft;
}
