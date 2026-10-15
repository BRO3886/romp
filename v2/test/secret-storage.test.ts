import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createCredentials } from '../src/adapters/node/auth.js';
import {
  MissingSecretError,
  SecretStorage,
  type VaultEntry,
} from '../src/adapters/node/secret-storage.js';

async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), 'romp-secret-storage-'));
  const values = new Map<string, string>();
  let denyWrite = false;
  let denyRead = false;
  let denyDelete = false;
  let discardWrite = false;
  const entry = (account: string): Promise<VaultEntry> =>
    Promise.resolve({
      getPassword: () => {
        if (denyRead) throw new Error('Vault locked');
        return Promise.resolve(values.get(account) ?? null);
      },
      setPassword: (value, signal) => {
        signal?.throwIfAborted();
        if (denyWrite) throw new Error('Vault denied');
        if (!discardWrite) values.set(account, value);
        return Promise.resolve();
      },
      deleteCredential: () => {
        if (denyDelete) throw new Error('Vault denied');
        return Promise.resolve(values.delete(account));
      },
    });
  return {
    directory,
    values,
    options: { mode: 'keyring' as const, entry },
    denyWrite: (value: boolean) => {
      denyWrite = value;
    },
    denyRead: (value: boolean) => {
      denyRead = value;
    },
    denyDelete: (value: boolean) => {
      denyDelete = value;
    },
    discardWrite: () => {
      discardWrite = true;
    },
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}
await test('vault credentials serialize mutation, preserve providers, and leave only non-secret references', async () => {
  const f = await fixture();
  try {
    const directory = join(f.directory, 'state');
    const first = createCredentials(directory, f.options);
    const second = createCredentials(directory, f.options);
    assert.equal(await first.read('openai'), undefined);
    await assert.rejects(stat(directory), { code: 'ENOENT' });
    assert.equal(f.values.size, 0);
    await Promise.all([
      first.modify('openai', () =>
        Promise.resolve({
          type: 'oauth',
          access: 'secret-access',
          refresh: 'secret-refresh',
          expires: 1,
        }),
      ),
      second.modify('anthropic', () => Promise.resolve({ type: 'api_key', key: 'secret-api-key' })),
    ]);
    assert.equal((await first.read('openai'))?.type, 'oauth');
    assert.equal((await second.read('anthropic'))?.type, 'api_key');
    const text = await readFile(join(directory, 'auth.json'), 'utf8');
    assert.doesNotMatch(text, /secret-|refresh|access|api_key/);
    assert.match(text, /"storage":"keyring"/);
    assert.equal((await stat(join(directory, 'auth.json'))).mode & 0o777, 0o600);
    await first.delete('openai');
    assert.equal(await first.read('openai'), undefined);
    assert.equal((await first.read('anthropic'))?.type, 'api_key');
    await second.delete('anthropic');
    assert.equal(f.values.size, 0);
    await assert.rejects(stat(join(directory, 'auth.json')), { code: 'ENOENT' });
  } finally {
    await f.cleanup();
  }
});
await test('migration preserves the plaintext source until the vault acknowledges storage', async () => {
  const f = await fixture();
  try {
    const file = join(f.directory, 'legacy.json');
    const legacy = JSON.stringify({ refresh: 'legacy-secret' });
    await writeFile(file, legacy, { mode: 0o600 });
    const storage = new SecretStorage(file, f.options);
    f.denyWrite(true);
    await assert.rejects(storage.read(), /vault.*unavailable/);
    assert.equal(await readFile(file, 'utf8'), legacy);
    assert.equal(f.values.size, 0);
    f.denyWrite(false);
    assert.deepEqual(await storage.read(), { refresh: 'legacy-secret' });
    assert.ok(!(await readFile(file, 'utf8')).includes('legacy-secret'));
    f.denyRead(true);
    await assert.rejects(storage.read(), /vault.*unavailable/);
    assert.ok(!(await readFile(file, 'utf8')).includes('legacy-secret'));
    f.denyRead(false);
    await storage.delete();
    assert.equal(f.values.size, 0);
  } finally {
    await f.cleanup();
  }
});
await test('vault readback, denial, and missing entries never trigger a plaintext downgrade', async () => {
  const f = await fixture();
  try {
    const file = join(f.directory, 'profile.json');
    const storage = new SecretStorage(file, f.options);
    await storage.write({ token: 'protected-secret' });
    const fallback = new SecretStorage(file, { ...f.options, mode: 'file' });
    await fallback.write({ token: 'rotated-secret' });
    assert.deepEqual(await storage.read(), { token: 'rotated-secret' });
    assert.ok(!(await readFile(file, 'utf8')).includes('rotated-secret'));
    f.denyDelete(true);
    await assert.rejects(storage.delete(), /vault.*unavailable/);
    assert.deepEqual(await storage.read(), { token: 'rotated-secret' });
    f.denyDelete(false);
    f.values.clear();
    await assert.rejects(storage.read(), MissingSecretError);
    await storage.delete();
    f.discardWrite();
    await assert.rejects(storage.write({ token: 'unacknowledged' }), /vault.*unavailable/);
    await assert.rejects(stat(file), { code: 'ENOENT' });
    assert.equal(f.values.size, 0);
  } finally {
    await f.cleanup();
  }
});
await test('secret storage rejects symbolic links and cancellation before provisioning', async () => {
  const f = await fixture();
  try {
    const file = join(f.directory, 'secret.json');
    const link = join(f.directory, 'link.json');
    await writeFile(file, '{}', { mode: 0o600 });
    await symlink(file, link);
    await assert.rejects(new SecretStorage(link, f.options).read(), /ELOOP/);
    const controller = new AbortController();
    controller.abort(new Error('Cancelled before storage'));
    await assert.rejects(
      new SecretStorage(join(f.directory, 'missing/secret.json'), f.options).read(
        controller.signal,
      ),
      /Cancelled/,
    );
    await assert.rejects(stat(join(f.directory, 'missing')), { code: 'ENOENT' });
  } finally {
    await f.cleanup();
  }
});
await test('credential rotation persists after the refresh callback cancels its original request', async () => {
  const f = await fixture();
  try {
    const store = createCredentials(f.directory, f.options);
    const controller = new AbortController();
    await store.modify(
      'openai',
      () => {
        controller.abort(new Error('Request cancelled after token rotation'));
        return Promise.resolve({
          type: 'oauth',
          access: 'rotated-access',
          refresh: 'rotated-refresh',
          expires: 1,
        });
      },
      { signal: controller.signal },
    );
    const credential = await createCredentials(f.directory, f.options).read('openai');
    assert.ok(credential?.type === 'oauth');
    assert.equal(credential.refresh, 'rotated-refresh');
  } finally {
    await f.cleanup();
  }
});
