import { randomBytes } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { parseConfig } from '../../../src/domain/config.js';
import { validateAuthentication } from '../config.js';
import { checkModels, checkModelSelection } from '../models.js';
import { appJwt } from '../github.js';
import { SetupDraft, type Profile } from './profile.js';
import { originRepository, repositoryRoot } from './project.js';
import { hasErrorCode } from '../../../src/shared/errors.js';

export type SetupOptions = {
  openaiAuth?: string | undefined;
  account?: string | undefined;
  name?: string | undefined;
  appId?: string | undefined;
  privateKey?: string | undefined;
  repo?: string[] | undefined;
  config?: string | undefined;
  dockerfile?: string | undefined;
  concurrency?: number | undefined;
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
      (values.appId && values.appId !== previous.appId))
  )
    throw new Error(
      'Account, Worker name, and App ID are fixed for a profile. Select a new --profile',
    );
  const accountId = values.account ?? previous?.accountId;
  const name = values.name ?? previous?.name;
  const appId = values.appId ?? previous?.appId;
  const missing = [
    !accountId && '--account <ID>: Cloudflare account ID',
    !name && '--name <WORKER>: name for your Cloudflare Worker',
    !appId && '--app-id <ID>: GitHub App ID',
    !values.privateKey &&
      !context.env['GITHUB_PRIVATE_KEY'] &&
      !previous?.secrets.GITHUB_PRIVATE_KEY &&
      '--private-key <FILE>: GitHub App private key (or set GITHUB_PRIVATE_KEY)',
  ].filter(Boolean);
  if (missing.length)
    throw new Error(
      `Cloud setup needs deployment settings:\n${missing.map((value) => `  ${value}`).join('\n')}\n\nRun romp cloud setup --help for prerequisites and an example. Model login does not configure a Cloudflare deployment.`,
    );
  const root = await repositoryRoot(context.cwd);
  const configFile = values.config
    ? resolve(context.cwd, values.config)
    : (previous?.configFile ?? resolve(root, 'romp-v2.toml'));
  let config;
  try {
    config = parseConfig(await readFile(configFile, 'utf8'));
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT'))
      throw new Error(
        `No configuration at ${configFile}. Run romp init in the repository, or pass --config FILE.`,
        { cause: error },
      );
    throw error;
  }
  const repos = values.repo ??
    previous?.repositories.map((r) => r.repo) ?? [await originRepository(root)];
  const mode = values.openaiAuth ?? previous?.openaiAuth ?? 'api-key';
  const parsed = SetupDraft.safeParse({
    openaiAuth: mode,
    accountId,
    name,
    appId,
    configFile,
    dockerfile: resolve(
      context.cwd,
      values.dockerfile ?? previous?.dockerfile ?? resolve(context.root, 'Dockerfile'),
    ),
    concurrency: values.concurrency ?? previous?.concurrency ?? 1,
    repositories: repos.map((repo) => ({
      repo,
      config: { ...config, review: false },
    })),
    ...(previous?.endpoint ? { endpoint: previous.endpoint } : {}),
    secrets: {
      GITHUB_PRIVATE_KEY: values.privateKey
        ? await readFile(resolve(context.cwd, values.privateKey), 'utf8')
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
  if (!parsed.success) {
    const fields: Record<string, string> = {
      accountId: '--account',
      name: '--name',
      appId: '--app-id',
      repositories: '--repo',
      secrets: '--private-key or provider authentication',
      openaiAuth: '--openai-auth',
      concurrency: '--concurrency',
    };
    throw new Error(
      `Invalid cloud setup:\n${parsed.error.issues.map((issue) => `  ${fields[String(issue.path[0])] ?? 'configuration'}: ${issue.message}`).join('\n')}\nRun romp cloud setup --help for usage.`,
    );
  }
  const draft = parsed.data;
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
