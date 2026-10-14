import { readFile, mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { parse, type ParseError } from 'jsonc-parser';
import { z } from 'zod';
import { command } from '../../../src/adapters/node/process.js';
import { environment, type Profile } from './profile.js';

export type Runner = (args: string[], cwd: string, env: NodeJS.ProcessEnv) => Promise<void>;
export const runWrangler: Runner = async (args, cwd, env) => {
  const secretsFile = args[args.indexOf('--secrets-file') + 1];
  if (!secretsFile) throw new Error('Deployment requires a secrets file');
  const secrets = z
    .record(z.string(), z.string())
    .parse(JSON.parse(await readFile(secretsFile, 'utf8')) as unknown);
  const values = [...Object.values(secrets), env['CLOUDFLARE_API_TOKEN']].filter(
    (v): v is string => !!v,
  );
  const redact = (text: string) =>
    values.reduce(
      (s, v) => s.replaceAll(v, '(hidden)').replaceAll(JSON.stringify(v).slice(1, -1), '(hidden)'),
      text,
    );
  const result = await command(
    process.execPath,
    [resolve(cwd, '../node_modules/wrangler/bin/wrangler.js'), ...args],
    cwd,
    AbortSignal.timeout(600_000),
    { env: { ...env, WRANGLER_LOG: 'info' }, allowFailure: true },
  );
  process.stdout.write(redact(result.stdout));
  process.stderr.write(redact(result.stderr));
  if (result.code)
    throw new Error(`Wrangler exited ${result.code}\n${redact(result.stderr || result.stdout)}`);
};
export async function deploymentConfig(root: string, profile: Profile) {
  const errors: ParseError[] = [];
  const raw: unknown = parse(await readFile(join(root, 'wrangler.jsonc'), 'utf8'), errors, {
    allowTrailingComma: true,
  });
  if (errors.length) throw new Error('Invalid Wrangler template');
  const template = z
    .object({
      main: z.string(),
      containers: z.array(z.object({ class_name: z.string() }).passthrough()).length(1),
    })
    .passthrough()
    .parse(raw);
  return {
    ...template,
    main: resolve(root, template.main),
    name: profile.name,
    account_id: profile.accountId,
    workers_dev: true,
    vars: {
      REPOSITORIES: environment(profile).REPOSITORIES,
      CONCURRENCY: String(profile.concurrency),
      GITHUB_APP_ID: profile.appId,
    },
    containers: template.containers.map((c) => ({
      ...c,
      images: { runner: { dockerfile: profile.dockerfile } },
    })),
  };
}
export async function deploy(
  root: string,
  profile: Profile,
  env: NodeJS.ProcessEnv,
  dryRun: boolean,
  runner: Runner,
) {
  const config = await deploymentConfig(root, profile);
  const directory = await mkdtemp(join(tmpdir(), 'romp-cloud-'));
  try {
    const file = join(directory, 'wrangler.json');
    const secrets = join(directory, 'secrets.json');
    await writeFile(file, JSON.stringify(config), { mode: 0o600 });
    await writeFile(secrets, JSON.stringify(profile.secrets), { mode: 0o600 });
    await runner(
      [
        'deploy',
        '--config',
        file,
        '--secrets-file',
        secrets,
        ...(dryRun ? ['--dry-run', '--outdir', join(directory, 'bundle')] : []),
      ],
      root,
      {
        ...env,
        CLOUDFLARE_ACCOUNT_ID: profile.accountId,
        WRANGLER_SEND_METRICS: 'false',
        CI: 'true',
      },
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
