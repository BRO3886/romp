import { join } from 'node:path';
import { SecretStorage } from '../../../src/adapters/node/secret-storage.js';
export { privateDirectory } from '../../../src/adapters/node/private-files.js';
import { z } from 'zod';
import { OpenAIAuth } from '../subscription-schema.js';
import { CloudConfig, Repository, type Secrets } from '../config.js';

export const Profile = CloudConfig.safeExtend({
  openaiAuth: OpenAIAuth.default('api-key'),
  accountId: z.string().regex(/^[a-f0-9]{32}$/),
  name: z.string().regex(/^[a-z][a-z0-9-]{0,62}$/),
  appId: z.string().regex(/^[1-9]\d*$/),
  configFile: z.string().min(1),
  dockerfile: z.string().min(1),
  endpoint: z
    .string()
    .regex(/^https:\/\/[a-z0-9-]+\.[a-z0-9-]+\.workers\.dev$/)
    .optional(),
  secrets: z
    .object({
      GITHUB_PRIVATE_KEY: z.string().min(1),
      GITHUB_WEBHOOK_SECRET: z.string().min(32),
      ADMIN_TOKEN: z.string().min(32),
      SUBSCRIPTION_KEY: z
        .string()
        .regex(/^[a-f0-9]{64}$/)
        .optional(),
      OPENAI_API_KEY: z.string().min(1).optional(),
      ANTHROPIC_API_KEY: z.string().min(1).optional(),
    })
    .strict()
    .transform((v): Secrets => ({
      GITHUB_PRIVATE_KEY: v.GITHUB_PRIVATE_KEY,
      GITHUB_WEBHOOK_SECRET: v.GITHUB_WEBHOOK_SECRET,
      ADMIN_TOKEN: v.ADMIN_TOKEN,
      ...(v.SUBSCRIPTION_KEY ? { SUBSCRIPTION_KEY: v.SUBSCRIPTION_KEY } : {}),
      ...(v.OPENAI_API_KEY ? { OPENAI_API_KEY: v.OPENAI_API_KEY } : {}),
      ...(v.ANTHROPIC_API_KEY ? { ANTHROPIC_API_KEY: v.ANTHROPIC_API_KEY } : {}),
    })),
}).strict();
export type Profile = z.infer<typeof Profile>;
export const SetupDraft = z
  .object({
    ...Profile.shape,
    repositories: z.array(Repository.omit({ installationId: true })).min(1),
  })
  .strict()
  .refine(
    (v) => new Set(v.repositories.map((r) => r.repo.toLowerCase())).size === v.repositories.length,
    'Duplicate repository',
  );
export type SetupDraft = z.infer<typeof SetupDraft>;

export function profilePath(home: string, name: string) {
  if (!/^[a-zA-Z0-9_-]+$/.test(name)) throw new Error('Invalid profile name');
  return join(home, '.romp-v2', 'cloud', `${name}.json`);
}
export async function loadProfile(file: string): Promise<Profile | undefined> {
  const value = await new SecretStorage(file).read();
  return value === undefined ? undefined : Profile.parse(value);
}
export async function saveProfile(file: string, profile: Profile) {
  await new SecretStorage(file).write(Profile.parse(profile));
}
export function environment(profile: Profile) {
  return {
    REPOSITORIES: JSON.stringify(profile.repositories),
    CONCURRENCY: String(profile.concurrency),
    GITHUB_APP_ID: profile.appId,
    OPENAI_AUTH: profile.openaiAuth,
    ...profile.secrets,
  };
}
