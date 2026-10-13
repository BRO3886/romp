import { hasErrorCode } from '../../shared/errors.js';
import { z } from 'zod';
import { parseJson } from '../../shared/json.js';
import { randomUUID } from 'node:crypto';
import { chmod, mkdir, open, readFile, rename, unlink, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import type {
  AuthOperationOptions,
  Credential,
  CredentialInfo,
  CredentialStore,
} from '@earendil-works/pi-ai';
import { removeDeadLock } from './ownership.js';

const CredentialSchema = z
  .discriminatedUnion('type', [
    z
      .object({
        type: z.literal('api_key'),
        key: z.string().optional(),
        env: z.record(z.string(), z.string()).optional(),
      })
      .passthrough(),
    z
      .object({
        type: z.literal('oauth'),
        access: z.string(),
        refresh: z.string(),
        expires: z.number(),
      })
      .passthrough(),
  ])
  .transform((credential): Credential => {
    if (credential.type === 'oauth') return credential;
    const { key, env, ...rest } = credential;
    return {
      ...rest,
      ...(key === undefined ? {} : { key }),
      ...(env === undefined ? {} : { env }),
    };
  });
const CredentialsSchema = z.record(z.string(), CredentialSchema);
export class FileCredentials implements CredentialStore {
  private chain: Promise<unknown> = Promise.resolve();
  constructor(private readonly directory: string) {}
  private async load(): Promise<Record<string, Credential>> {
    try {
      return CredentialsSchema.parse(
        parseJson(await readFile(join(this.directory, 'auth.json'), 'utf8')),
      );
    } catch (error) {
      if (hasErrorCode(error, 'ENOENT')) return {};
      throw error;
    }
  }
  async read(provider: string, options?: AuthOperationOptions) {
    options?.signal?.throwIfAborted();
    return (await this.load())[provider];
  }
  async list(options?: AuthOperationOptions): Promise<CredentialInfo[]> {
    options?.signal?.throwIfAborted();
    return Object.entries(await this.load()).map(([providerId, credential]) => ({
      providerId,
      type: credential.type,
    }));
  }
  private serialize<T>(action: () => Promise<T>): Promise<T> {
    const pending = this.chain.then(action);
    this.chain = pending.catch(() => {});
    return pending;
  }
  private async lock(options?: AuthOperationOptions) {
    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    await chmod(this.directory, 0o700);
    const path = join(this.directory, 'auth.lock');
    for (let attempt = 0; attempt < 300; attempt++) {
      options?.signal?.throwIfAborted();
      try {
        const file = await open(path, 'wx', 0o600);
        try {
          await file.writeFile(String(process.pid));
          await file.sync();
        } finally {
          await file.close();
        }
        return async () => {
          await unlink(path);
        };
      } catch (error) {
        if (!hasErrorCode(error, 'EEXIST')) throw error;
        let pid: number;
        try {
          pid = Number(await readFile(path, 'utf8'));
        } catch (readError) {
          if (!hasErrorCode(readError, 'ENOENT')) throw readError;
          continue;
        }
        if (!Number.isSafeInteger(pid) || pid < 1) {
          await delay(100, undefined, { signal: options?.signal });
          continue;
        }
        if (removeDeadLock(path)) continue;
        await delay(100, undefined, { signal: options?.signal });
      }
    }
    throw new Error('Credential store is busy');
  }
  private async save(data: Record<string, Credential>) {
    const temporary = join(this.directory, `auth-${randomUUID()}.tmp`);
    try {
      await writeFile(temporary, JSON.stringify(data), { mode: 0o600, flag: 'wx' });
      const file = await open(temporary, 'r');
      try {
        await file.sync();
      } finally {
        await file.close();
      }
      await rename(temporary, join(this.directory, 'auth.json'));
    } finally {
      await unlink(temporary).catch((error: NodeJS.ErrnoException) => {
        if (error.code !== 'ENOENT') throw error;
      });
    }
  }
  modify(
    provider: string,
    fn: (credential: Credential | undefined) => Promise<Credential | undefined>,
    options?: AuthOperationOptions,
  ) {
    return this.serialize(async () => {
      const unlock = await this.lock(options);
      try {
        const data = await this.load();
        const credential = await fn(data[provider]);
        if (credential !== undefined) {
          data[provider] = credential;
          await this.save(data);
        }
        return data[provider];
      } finally {
        await unlock();
      }
    });
  }
  delete(provider: string, options?: AuthOperationOptions) {
    return this.serialize(async () => {
      const unlock = await this.lock(options);
      try {
        const data = await this.load();
        delete data[provider];
        await this.save(data);
      } finally {
        await unlock();
      }
    });
  }
}
