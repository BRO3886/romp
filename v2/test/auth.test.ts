import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { FileCredentials } from '../src/auth.js';

test('credential reads provision nothing and concurrent mutations preserve all providers', async () => {
  const root = await mkdtemp(join(tmpdir(), 'romp-v2-auth-')); const directory = join(root, 'state');
  const first = new FileCredentials(directory); const second = new FileCredentials(directory);
  try {
    assert.equal(await first.read('openai'), undefined);
    await assert.rejects(stat(directory), { code: 'ENOENT' });
    await Promise.all([
      first.modify('openai', async () => { await delay(50); return { type: 'oauth', access: 'test-access', refresh: 'test-refresh', expires: 1 }; }),
      second.modify('anthropic', async () => ({ type: 'api_key', key: 'test-key' })),
    ]);
    assert.deepEqual((await first.list()).sort((a, b) => a.providerId.localeCompare(b.providerId)),
      [{ providerId: 'anthropic', type: 'api_key' }, { providerId: 'openai', type: 'oauth' }]);
    assert.equal((await stat(join(directory, 'auth.json'))).mode & 0o777, 0o600);
    await first.delete('openai');
    assert.equal(await first.read('openai'), undefined);
    assert.equal((await second.read('anthropic'))?.type, 'api_key');
    assert(!JSON.stringify(await first.list()).includes('test-key'));
    assert((await readFile(join(directory, 'auth.json'), 'utf8')).includes('anthropic'));
  } finally { await rm(root, { recursive: true, force: true }); }
});
