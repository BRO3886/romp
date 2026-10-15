import { z } from 'zod';
import { OpenAIAuth } from './subscription-schema.js';
import { ConfigSchema, IssueSchema } from '../../src/domain/model.js';

export const Repository = z
  .object({
    repo: z.string().regex(/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/),
    installationId: z.number().int().positive(),
    config: ConfigSchema.refine(
      (v) => !v.review,
      'Cloud runs finish at PR creation; configure review=false',
    ),
  })
  .strict();
export const CloudConfig = z
  .object({
    concurrency: z.number().int().min(1).max(32),
    repositories: z.array(Repository).min(1),
  })
  .strict()
  .refine(
    (v) => new Set(v.repositories.map((r) => r.repo.toLowerCase())).size === v.repositories.length,
    'Duplicate repository',
  );
export const Submission = z
  .object({
    id: z.string().regex(/^r\d+-i\d+$/),
    repositoryId: z.number().int().positive(),
    repository: Repository,
    issue: IssueSchema,
  })
  .strict();
export type Submission = z.infer<typeof Submission>;
export type CloudConfig = z.infer<typeof CloudConfig>;
export type Secrets = {
  GITHUB_WEBHOOK_SECRET: string;
  GITHUB_PRIVATE_KEY: string;
  ADMIN_TOKEN: string;
  SUBSCRIPTION_KEY?: string;
  OPENAI_API_KEY?: string;
  ANTHROPIC_API_KEY?: string;
};
export type Authentication = Secrets & { OPENAI_AUTH?: string };
export type Bindings = {
  [K in keyof Cloudflare.Env]: Cloudflare.Env[K] extends string ? string : Cloudflare.Env[K];
} & Secrets;
export function configuration(env: Pick<Bindings, 'REPOSITORIES' | 'CONCURRENCY'>): CloudConfig {
  return CloudConfig.parse({
    concurrency: Number(env.CONCURRENCY),
    repositories: JSON.parse(env.REPOSITORIES) as unknown,
  });
}
export function runId(repositoryId: number, issue: number) {
  return `r${repositoryId}-i${issue}`;
}
export function preflight(
  env: Pick<Bindings, 'REPOSITORIES' | 'CONCURRENCY' | 'GITHUB_APP_ID'> & Authentication,
) {
  const config = configuration(env);
  validateAuthentication(config, env);
  return config;
}
export function validateAuthentication(
  config: { repositories: readonly { config: { model: { provider: string } } }[] },
  env: { GITHUB_APP_ID: string } & Authentication,
) {
  if (!/^\d+$/.test(env.GITHUB_APP_ID)) throw new Error('GITHUB_APP_ID must be a numeric app ID');
  for (const value of [env.GITHUB_WEBHOOK_SECRET, env.GITHUB_PRIVATE_KEY, env.ADMIN_TOKEN]) {
    if (!value?.trim())
      throw new Error('GitHub credentials, webhook secret, and admin token are required');
  }
  if (env.ADMIN_TOKEN.length < 32 || env.GITHUB_WEBHOOK_SECRET.length < 32)
    throw new Error('Admin and webhook secrets must contain at least 32 characters');
  const mode = OpenAIAuth.parse(env.OPENAI_AUTH ?? 'api-key');
  if (mode === 'subscription' && !/^[a-f0-9]{64}$/.test(env.SUBSCRIPTION_KEY ?? ''))
    throw new Error('SUBSCRIPTION_KEY must contain 32 random bytes in hexadecimal');
  for (const repository of config.repositories) {
    const model = repository.config.model;
    if (model.provider !== 'openai' && model.provider !== 'anthropic')
      throw new Error('Supported providers: openai, anthropic');
    if (model.provider === 'openai' && mode === 'subscription') continue;
    if (!(model.provider === 'openai' ? env.OPENAI_API_KEY : env.ANTHROPIC_API_KEY))
      throw new Error(`Missing API key for ${model.provider}`);
  }
}
