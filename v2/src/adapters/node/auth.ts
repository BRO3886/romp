import { z } from 'zod';
import { join } from 'node:path';
import type {
  AuthOperationOptions,
  Credential,
  CredentialInfo,
  CredentialStore,
} from '@earendil-works/pi-ai';
import {
  MissingSecretError,
  removeSecret,
  SecretStorage,
  type SecretStorageOptions,
} from './secret-storage.js';

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
class StoredCredentials implements CredentialStore {
  constructor(private readonly storage: SecretStorage) {}
  private data(value: unknown): Record<string, Credential> {
    return value === undefined ? {} : CredentialsSchema.parse(value);
  }
  private async load(options?: AuthOperationOptions) {
    try {
      return this.data(await this.storage.read(options?.signal));
    } catch (error) {
      if (error instanceof MissingSecretError) return {};
      throw error;
    }
  }
  async read(provider: string, options?: AuthOperationOptions) {
    return (await this.load(options))[provider];
  }
  async list(options?: AuthOperationOptions): Promise<CredentialInfo[]> {
    return Object.entries(await this.load(options)).map(([providerId, credential]) => ({
      providerId,
      type: credential.type,
    }));
  }
  async modify(
    provider: string,
    fn: (credential: Credential | undefined) => Promise<Credential | undefined>,
    options?: AuthOperationOptions,
  ) {
    const result = await this.storage.update(async (value) => {
      const data = this.data(value);
      const credential = await fn(data[provider]);
      if (credential === undefined) return undefined;
      data[provider] = credential;
      return data;
    }, options?.signal);
    return this.data(result)[provider];
  }
  async delete(provider: string, options?: AuthOperationOptions) {
    await this.storage.update((value) => {
      if (value === undefined) return Promise.resolve(undefined);
      const data = this.data(value);
      delete data[provider];
      return Promise.resolve(Object.keys(data).length ? data : removeSecret);
    }, options?.signal);
  }
}
export function createCredentials(directory: string, options: SecretStorageOptions = {}) {
  return new StoredCredentials(new SecretStorage(join(directory, 'auth.json'), options));
}
export class FileCredentials extends StoredCredentials {
  constructor(directory: string) {
    super(new SecretStorage(join(directory, 'auth.json'), { mode: 'file' }));
  }
}
