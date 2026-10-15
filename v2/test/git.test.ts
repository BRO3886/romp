import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { Git } from '../src/adapters/node/git.js';
import { command } from '../src/adapters/node/process.js';
import { fixture } from './fixture.js';

await test('concurrent base resolution returns each remote branch tip without changing shared fetch state', async () => {
  const f = await fixture();
  try {
    const main = (await command('git', ['rev-parse', 'HEAD'], f.root)).stdout.trim();
    await command('git', ['checkout', '-b', 'alternate'], f.root);
    await writeFile(join(f.root, 'answer.txt'), 'alternate');
    await command('git', ['add', 'answer.txt'], f.root);
    await command('git', ['commit', '-m', 'test: create alternate base'], f.root);
    const alternate = (await command('git', ['rev-parse', 'HEAD'], f.root)).stdout.trim();
    await command('git', ['push', 'origin', 'alternate'], f.root);
    await command('git', ['checkout', 'main'], f.root);
    await command('git', ['fetch', 'origin', 'alternate'], f.root);
    const file = join(f.root, '.git', 'FETCH_HEAD');
    const previous = await readFile(file, 'utf8');
    assert.equal(await new Git().base(f.root, 'main'), main);
    assert.equal(await readFile(file, 'utf8'), previous);
    assert.equal(await new Git().base(f.root, 'refs/heads/main'), main);
    const branches = Array.from({ length: 8 }, (_, i) => (i % 2 ? 'alternate' : 'main'));
    assert.deepEqual(
      await Promise.all(branches.map((branch) => new Git().base(f.root, branch))),
      branches.map((branch) => (branch === 'main' ? main : alternate)),
    );
    assert.equal(await readFile(file, 'utf8'), previous);
    await assert.rejects(new Git().base(f.root, 'missing-branch'), /couldn't find remote ref/);
    assert.equal((await command('git', ['for-each-ref', 'refs/romp/fetch'], f.root)).stdout, '');
  } finally {
    await f.cleanup();
  }
});
