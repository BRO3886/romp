import assert from 'node:assert/strict';
import test from 'node:test';
import { createServer } from 'node:net';
import { mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { z } from 'zod';
import { createClient } from '../src/adapters/node/client.js';
import { parseRequest } from '../src/transport/protocol.js';
import { parseJson } from '../src/shared/json.js';
import { CheckpointSchema, initialCheckpoint } from '../src/domain/checkpoint.js';

await test('protocol rejects coercible issue IDs and unexpected request fields', () => {
  const request = {
    version: 2,
    id: 'request',
    method: 'run',
    params: { root: '/repo', issue: 17 },
  };
  assert.deepEqual(parseRequest(JSON.stringify(request)), request);
  for (const issue of [true, '17', 0, 1.5, null]) {
    assert.throws(() =>
      parseRequest(JSON.stringify({ ...request, params: { root: '/repo', issue } })),
    );
  }
  assert.throws(() =>
    parseRequest(
      JSON.stringify({ ...request, params: { root: '/repo', issue: 17, skipVerification: true } }),
    ),
  );
});
await test('workflow checkpoint phases require the data their next action consumes', () => {
  const initial = initialCheckpoint();
  assert.throws(() => CheckpointSchema.parse({ ...initial, phase: 'building' }));
  assert.throws(() => CheckpointSchema.parse({ ...initial, phase: 'publish', sha: 'commit' }));
  assert.throws(() => CheckpointSchema.parse({ ...initial, phase: 'finish' }));
  assert.equal(CheckpointSchema.parse(initial).phase, 'prepare');
});
await test('typed client rejects malformed success payloads over a real Unix socket', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'romp-v2-protocol-'));
  const server = createServer((socket) => {
    socket.once('data', (buffer: Buffer) => {
      const request = z.object({ id: z.string() }).parse(parseJson(buffer.toString()));
      socket.end(JSON.stringify({ version: 2, id: request.id, result: { version: 123 } }) + '\n');
    });
  });
  try {
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject);
      server.listen(join(directory, 'romp.sock'), resolve);
    });
    await assert.rejects(createClient(directory).ping({}), /string/);
  } finally {
    if (server.listening) await new Promise<void>((resolve) => server.close(() => resolve()));
    await rm(directory, { recursive: true, force: true });
  }
});

await test('job lookup rejects inherited dictionary properties', async () => {
  const { fixture, scriptedModels } = await import('./fixture.js');
  const { openLocalRuntime } = await import('../src/composition/node.js');
  const f = await fixture();
  const { models } = scriptedModels();
  const runtime = await openLocalRuntime(f.state, f.gh, { models });
  try {
    for (const id of ['toString', 'constructor', '__proto__']) {
      await assert.rejects(runtime.job(id), /Unknown job/);
    }
  } finally {
    await runtime.close();
    await f.cleanup();
  }
});
