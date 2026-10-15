import { constants } from 'node:fs';
import { chmod, lstat, mkdir, open, rename, rm } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { join } from 'node:path';
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
export async function readPrivateJson(file: string): Promise<unknown> {
  try {
    const handle = await open(file, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = await handle.stat();
      if (!stat.isFile() || stat.mode & 0o077)
        throw new Error('Cloud profile must be a private file (chmod 600)');
      return JSON.parse(await handle.readFile('utf8')) as unknown;
    } finally {
      await handle.close();
    }
  } catch (error) {
    if (error instanceof Error && 'code' in error && error.code === 'ENOENT') return undefined;
    throw error;
  }
}
export async function loadProfile(file: string): Promise<Profile | undefined> {
  const value = await readPrivateJson(file);
  return value === undefined ? undefined : Profile.parse(value);
}
export async function privateDirectory(path: string) {
  await mkdir(path, { recursive: true, mode: 0o700 });
  if (!(await lstat(path)).isDirectory())
    throw new Error('Profile directory must not be a symlink');
  await chmod(path, 0o700);
}
export async function savePrivateJson(file: string, value: unknown) {
  const temp = `${file}.${randomBytes(8).toString('hex')}`;
  try {
    const handle = await open(temp, 'wx', 0o600);
    try {
      await handle.writeFile(`${JSON.stringify(value, null, 2)}\n`);
      await handle.sync();
    } finally {
      await handle.close();
    }
    await rename(temp, file);
  } finally {
    await rm(temp, { force: true });
  }
}
export async function saveProfile(file: string, profile: Profile) {
  await savePrivateJson(file, Profile.parse(profile));
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
