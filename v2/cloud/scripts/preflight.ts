import { checkModels } from '../src/models.js';
import { readFile, access } from 'node:fs/promises';
import { loadEnvFile } from 'node:process';
import { z } from 'zod';
import { parse, type ParseError } from 'jsonc-parser';
import { appJwt } from '../src/github.js';
import { preflight } from '../src/config.js';
const file = process.argv[2] ?? 'wrangler.jsonc';
const errors: ParseError[] = [];
const raw: unknown = parse(await readFile(file, 'utf8'), errors, { allowTrailingComma: true });
if (errors.length) throw new Error('Invalid Wrangler configuration');
const configuration = z
  .object({
    vars: z.object({
      REPOSITORIES: z.string(),
      CONCURRENCY: z.string(),
      GITHUB_APP_ID: z.string(),
    }),
  })
  .parse(raw);
try {
  await access('.dev.vars');
  loadEnvFile('.dev.vars');
} catch (error) {
  if (!(error instanceof Error) || !('code' in error) || error.code !== 'ENOENT') throw error;
}
const environment = {
  ...configuration.vars,
  GITHUB_WEBHOOK_SECRET: process.env['GITHUB_WEBHOOK_SECRET'] ?? '',
  GITHUB_PRIVATE_KEY: process.env['GITHUB_PRIVATE_KEY'] ?? '',
  ADMIN_TOKEN: process.env['ADMIN_TOKEN'] ?? '',
  ...(process.env['OPENAI_API_KEY'] ? { OPENAI_API_KEY: process.env['OPENAI_API_KEY'] } : {}),
  ...(process.env['ANTHROPIC_API_KEY']
    ? { ANTHROPIC_API_KEY: process.env['ANTHROPIC_API_KEY'] }
    : {}),
};
const resolved = preflight(environment);
await appJwt(environment.GITHUB_APP_ID, environment.GITHUB_PRIVATE_KEY);
await checkModels(resolved, environment);
console.log(
  JSON.stringify({
    ready: true,
    concurrency: resolved.concurrency,
    repositories: resolved.repositories.map((r) => r.repo),
    provisions: false,
  }),
);
