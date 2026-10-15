import { randomUUID } from 'node:crypto';
import { basename } from 'node:path';
import { rm } from 'node:fs/promises';
import { z } from 'zod';
import { readPrivateJson, savePrivateJson } from './private-files.js';
import { withSecretLock } from './secret-lock.js';

export const KEYRING_SERVICE = 'com.romp.v2';
const Reference = z
  .object({ storage: z.literal('keyring'), version: z.literal(1), account: z.string().min(1) })
  .strict();
export const removeSecret = Symbol('remove-secret');
export type VaultEntry = {
  getPassword(signal?: AbortSignal): Promise<string | null>;
  setPassword(password: string, signal?: AbortSignal): Promise<void>;
  deleteCredential(signal?: AbortSignal): Promise<boolean>;
};
export type SecretStorageOptions = {
  mode?: 'keyring' | 'file';
  entry?: (account: string) => Promise<VaultEntry>;
};
export class MissingSecretError extends Error {}
function reference(value: unknown) {
  if (value && typeof value === 'object' && 'storage' in value && value.storage === 'keyring')
    return Reference.parse(value);
  return undefined;
}
async function nativeEntry(account: string): Promise<VaultEntry> {
  const { AsyncEntry } = await import('@napi-rs/keyring');
  return new AsyncEntry(KEYRING_SERVICE, account, { linux: { store: 'secret-service' } });
}
export class SecretStorage {
  private readonly mode: 'keyring' | 'file';
  private readonly entry: (account: string) => Promise<VaultEntry>;
  constructor(
    readonly file: string,
    options: SecretStorageOptions = {},
  ) {
    const mode = options.mode ?? process.env['ROMP_SECRET_STORAGE'] ?? 'keyring';
    if (mode !== 'keyring' && mode !== 'file')
      throw new Error('ROMP_SECRET_STORAGE must be keyring or file');
    this.mode = mode;
    this.entry = options.entry ?? nativeEntry;
  }
  async location(): Promise<string> {
    const stored = reference(await readPrivateJson(this.file));
    if (!stored && this.mode === 'file') return `plaintext file ${this.file} (explicit fallback)`;
    const vault =
      process.platform === 'darwin'
        ? 'macOS Keychain'
        : process.platform === 'win32'
          ? 'Windows Credential Manager'
          : 'OS Secret Service';
    return `${vault} (reference: ${this.file})`;
  }
  async checkAvailable(): Promise<void> {
    const stored = reference(await readPrivateJson(this.file));
    if (stored || this.mode === 'keyring') {
      await this.vault(async () =>
        (await this.entry(stored?.account ?? 'availability-probe')).getPassword(),
      );
    }
  }
  private async vault<T>(action: () => Promise<T>): Promise<T> {
    try {
      return await action();
    } catch (error) {
      throw new Error(
        'Credential vault is unavailable or access was denied. Unlock your OS credential vault and retry. Headless environments can explicitly select plaintext storage with ROMP_SECRET_STORAGE=file.',
        { cause: error },
      );
    }
  }
  private async load(value: unknown, signal?: AbortSignal): Promise<unknown> {
    const stored = reference(value);
    if (!stored) return value;
    const text = await this.vault(async () =>
      (await this.entry(stored.account)).getPassword(signal),
    );
    if (text === null) return undefined;
    try {
      return JSON.parse(text) as unknown;
    } catch (error) {
      throw new Error(`Invalid credential data in the vault for ${this.file}`, { cause: error });
    }
  }
  async read(signal?: AbortSignal): Promise<unknown> {
    signal?.throwIfAborted();
    const raw = await readPrivateJson(this.file);
    if (raw === undefined) return undefined;
    if (!reference(raw) && this.mode === 'keyring')
      return this.update((value) => Promise.resolve(value), signal);
    const value = await this.load(raw, signal);
    if (value === undefined)
      throw new MissingSecretError(
        `Credential vault entry is missing for ${this.file}. Restore the entry or sign in again.`,
      );
    return value;
  }
  private async save(value: unknown, previous: unknown) {
    const stored = reference(previous);
    if (!stored && this.mode === 'file') {
      await savePrivateJson(this.file, value);
      return;
    }
    const account = stored?.account ?? `${basename(this.file)}:${randomUUID()}`;
    const text = JSON.stringify(value);
    if (text === undefined) throw new Error('Credential data cannot be empty');
    await this.vault(async () => {
      const entry = await this.entry(account);
      try {
        await entry.setPassword(text);
        if ((await entry.getPassword()) !== text)
          throw new Error('Credential vault did not acknowledge the stored value');
        if (!stored) await savePrivateJson(this.file, { storage: 'keyring', version: 1, account });
      } catch (error) {
        if (!stored) await entry.deleteCredential();
        throw error;
      }
    });
  }
  private async erase(raw: unknown) {
    const stored = reference(raw);
    if (stored) await this.vault(async () => (await this.entry(stored.account)).deleteCredential());
    await rm(this.file, { force: true });
  }
  async update(
    transform: (value: unknown) => Promise<unknown>,
    signal?: AbortSignal,
  ): Promise<unknown> {
    return withSecretLock(
      this.file,
      async () => {
        const raw = await readPrivateJson(this.file);
        const current = await this.load(raw, signal);
        const updated = await transform(current);
        if (updated === removeSecret) {
          await this.erase(raw);
          return undefined;
        }
        if (updated !== undefined) {
          // A rotated credential must commit even if its originating request was cancelled.
          await this.save(updated, raw);
          return updated;
        }
        return current;
      },
      signal,
    );
  }
  async write(value: unknown, signal?: AbortSignal) {
    await this.update(() => Promise.resolve(value), signal);
  }
  async delete(signal?: AbortSignal) {
    await withSecretLock(
      this.file,
      async () => this.erase(await readPrivateJson(this.file)),
      signal,
    );
  }
}
