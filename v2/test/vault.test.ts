import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { providerModels } from '../src/adapters/node/models.js';
import { SecretStorage } from '../src/adapters/node/secret-storage.js';
import { command } from '../src/adapters/node/process.js';

await test(
  'model authentication migrates legacy credentials into the native credential vault',
  {
    skip:
      process.platform !== 'darwin' &&
      process.env['ROMP_TEST_NATIVE_VAULT'] !== '1' &&
      'coverage gap: native credential vault not runnable on this host',
  },
  async () => {
    const directory = await mkdtemp(join(tmpdir(), 'romp-native-credentials-'));
    const file = join(directory, 'auth.json');
    const previous = process.env['ROMP_SECRET_STORAGE'];
    process.env['ROMP_SECRET_STORAGE'] = 'keyring';
    try {
      await writeFile(
        file,
        JSON.stringify({ openai: { type: 'api_key', key: 'romp-native-test-secret' } }),
        { mode: 0o600 },
      );
      const models = providerModels(directory);
      assert.ok(await models.getAuth('openai'));
      assert.equal((await readFile(file, 'utf8')).includes('romp-native-test-secret'), false);
      const worker = join(directory, 'read.mjs');
      await writeFile(
        worker,
        `import { createCredentials } from ${JSON.stringify(new URL('../src/adapters/node/auth.ts', import.meta.url).href)};
const credential = await createCredentials(process.argv[2]).read('openai');
if (credential?.type !== 'api_key' || credential.key !== 'romp-native-test-secret') throw new Error('Vault credential changed');
console.log('PASS separate-process vault read');`,
      );
      const result = await command(
        process.execPath,
        ['--import', 'tsx', worker, directory],
        resolve('.'),
      );
      assert.match(result.stdout, /PASS separate-process vault read/);
      await models.logout('openai');
    } finally {
      await new SecretStorage(file, { mode: 'keyring' }).delete();
      if (previous === undefined) delete process.env['ROMP_SECRET_STORAGE'];
      else process.env['ROMP_SECRET_STORAGE'] = previous;
      await rm(directory, { recursive: true, force: true });
    }
  },
);
await test(
  'native credential vault preserves large UTF-8 session values',
  {
    skip:
      process.platform !== 'darwin' &&
      process.env['ROMP_TEST_NATIVE_VAULT'] !== '1' &&
      'coverage gap: native credential vault not runnable on this host',
  },
  async () => {
    const directory = await mkdtemp(join(tmpdir(), 'romp-native-session-'));
    const storage = new SecretStorage(join(directory, 'session.json'), { mode: 'keyring' });
    try {
      const value = { refresh: `vault-test-${'session-\u03bb'.repeat(2048)}` };
      await storage.write(value);
      assert.deepEqual(await storage.read(), value);
      assert.ok(!(await readFile(storage.file, 'utf8')).includes('vault-test-'));
    } finally {
      await storage.delete();
      await rm(directory, { recursive: true, force: true });
    }
  },
);
