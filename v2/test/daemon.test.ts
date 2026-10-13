import { createClient } from '../src/adapters/node/client.js';
import { createConnection } from 'node:net';
import assert from 'node:assert/strict';
import test from 'node:test';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm, stat, writeFile, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { acquireLock } from '../src/adapters/node/owner-lock.js';
import { command } from '../src/adapters/node/process.js';

await test(
  'daemon protocol starts, rejects a second owner, stops, and reopens real SQLite',
  { timeout: 20_000 },
  async () => {
    const directory = await mkdtemp(join(tmpdir(), 'romp-v2-daemon-'));
    const start = async (compiled: boolean) => {
      const worker = spawn(
        process.execPath,
        [...(compiled ? ['dist/cli.js'] : ['--import', 'tsx', 'src/cli.ts']), 'daemon', 'start'],
        {
          cwd: process.cwd(),
          env: { ...process.env, ROMP_V2_STATE: directory },
          stdio: ['ignore', 'pipe', 'pipe'],
        },
      );
      let stderr = '';
      worker.stderr.on('data', (chunk: Buffer) => {
        stderr += chunk.toString();
      });
      const exited = new Promise<number | null>((resolve) => worker.once('exit', resolve));
      for (let i = 0; i < 100; i++) {
        try {
          await createClient(directory).ping({});
          return { worker, exited };
        } catch {
          if (worker.exitCode !== null) throw new Error(stderr);
          await delay(25);
        }
      }
      worker.kill('SIGKILL');
      throw new Error(`Daemon did not listen: ${stderr}`);
    };
    try {
      for (let i = 0; i < 2; i++) {
        const { worker, exited } = await start(i === 1);
        try {
          assert.deepEqual(await createClient(directory).ping({}), {
            version: '2.0.0-experimental.1',
          });
          await assert.rejects(acquireLock(directory), /already owned/);
          assert.match(await malformedRequest(directory), /Unknown method/);
          assert.equal((await stat(join(directory, 'romp.sock'))).mode & 0o777, 0o600);
          assert.deepEqual(await createClient(directory).state({}), { jobs: {}, repositories: {} });
          await createClient(directory).stop({});
          assert.equal(await exited, 0);
        } finally {
          if (worker.exitCode === null) worker.kill('SIGKILL');
        }
      }
      await assert.rejects(readFile(join(directory, 'owner.pid')), { code: 'ENOENT' });
      await writeFile(join(directory, 'owner.pid'), '99999999');
      const unlock = await acquireLock(directory);
      await unlock();
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  },
);
await test('compiled CLI runs through a bin symlink and compiled shell supervision preserves literal arguments', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'romp-v2-compiled-'));
  try {
    const link = join(directory, 'romp-v2');
    await symlink(resolve('dist/cli.js'), link);
    assert.match((await command(process.execPath, [link, '--help'], directory)).stdout, /Romp v2/);
    const { supervisedArguments } = await import('../dist/adapters/node/supervision.js');
    const result = await command(
      process.execPath,
      supervisedArguments(join(directory, 'executions'), `printf '%s' 'literal $(false)'`),
      directory,
    );
    assert.equal(result.stdout, 'literal $(false)');
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

function malformedRequest(directory: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const socket = createConnection(join(directory, 'romp.sock'));
    socket.once('error', reject);
    socket.once('connect', () =>
      socket.write(
        JSON.stringify({ version: 2, id: 'test', method: 'unsupported', params: {} }) + '\n',
      ),
    );
    let result = '';
    socket.on('data', (chunk: Buffer) => {
      result += chunk.toString();
    });
    socket.once('end', () => resolve(result));
  });
}
