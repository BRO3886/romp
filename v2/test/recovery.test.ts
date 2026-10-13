import assert from 'node:assert/strict';
import test from 'node:test';
import { spawn } from 'node:child_process';
import { readFile, stat, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai/providers/faux';
import { Runtime, preflight } from '../src/runtime.js';
import { fixture, scriptedModels, approve, buildResponses } from './fixture.js';
import { command } from '../src/process.js';

test('SIGKILL after PR creation resumes publication without a second PR or builder run', { timeout: 30_000 }, async () => {
  const f = await fixture();
  try {
    const worker = spawn(process.execPath, ['--import', 'tsx', 'test/crash-worker.ts', f.directory, f.root, f.state], { cwd: process.cwd(), stdio: ['ignore', 'pipe', 'pipe'] });
    let stderr = ''; worker.stderr.on('data', (b) => { stderr += b.toString(); });
    const result = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve, reject) => {
      worker.once('error', reject); worker.once('exit', (code, signal) => resolve({ code, signal }));
    });
    assert.equal(result.signal, 'SIGKILL', stderr);
    assert.equal((await f.gh.read()).creates, 1);
    const { models, faux } = scriptedModels(); faux.setResponses([approve()]);
    const runtime = await Runtime.open(f.state, f.gh, { models });
    try {
      const job = Object.values((await runtime.state()).jobs)[0]!;
      const finished = await runtime.wait(job.id);
      assert.equal(finished.outcome, 'done', finished.detail);
      assert.equal((await f.gh.read()).creates, 1);
      assert.equal(faux.state.callCount, 1);
      assert.equal(finished.events.filter((e) => e.phase === 'artifact').length, 1);
    } finally { await runtime.close(); }
  } finally { await f.cleanup(); }
});
test('cancellation reaches a running Pi shell tool and keeps its worktree', { timeout: 20_000 }, async () => {
  const f = await fixture(); const { models, faux } = scriptedModels();
  faux.setResponses([fauxAssistantMessage(fauxToolCall('bash', { command: 'touch started; sleep 30; touch escaped' }), { stopReason: 'toolUse' })]);
  const runtime = await Runtime.open(f.state, f.gh, { models });
  try {
    const queued = await runtime.enqueue(f.root, 17);
    const job = await runtime.job(queued.id);
    for (let i = 0; i < 100; i++) {
      try { await stat(join(job.worktree, 'started')); break; } catch { await delay(25); }
    }
    await stat(join(job.worktree, 'started'));
    await runtime.cancel(job.id);
    assert.equal((await runtime.wait(job.id)).outcome, 'cancelled');
    await assert.rejects(stat(join(job.worktree, 'escaped')), { code: 'ENOENT' });
    assert.equal((await f.gh.read()).creates, 0);
    await stat(job.worktree);
  } finally { await runtime.close(); await f.cleanup(); }
});
test('SIGKILL during a shell tool stops its old process group before agent recovery', { timeout: 30_000 }, async () => {
  const f = await fixture();
  try {
    const worker = spawn(process.execPath, ['--import', 'tsx', 'test/crash-worker.ts', f.directory, f.root, f.state, 'shell'], { cwd: process.cwd(), stdio: ['ignore', 'pipe', 'pipe'] });
    let stderr = ''; worker.stderr.on('data', (b) => { stderr += b.toString(); });
    const signal = await new Promise<NodeJS.Signals | null>((resolve, reject) => { worker.once('error', reject); worker.once('exit', (_code, signal) => resolve(signal)); });
    assert.equal(signal, 'SIGKILL', stderr);
    const { models, faux } = scriptedModels(); faux.setResponses([...buildResponses(), approve()]);
    const runtime = await Runtime.open(f.state, f.gh, { models });
    try {
      const job = Object.values((await runtime.state()).jobs)[0]!;
      const result = await runtime.wait(job.id);
      assert.equal(result.outcome, 'done', result.detail);
      const files = (await command('git', ['ls-tree', '-r', '--name-only', result.branch], f.remote)).stdout;
      assert(!files.includes('escaped'));
    } finally { await runtime.close(); }
  } finally { await f.cleanup(); }
});
test('preflight reads real configuration without provisioning state, worktrees, labels, or model calls', { timeout: 20_000 }, async () => {
  const f = await fixture(); const { models, faux } = scriptedModels();
  try {
    const result = await preflight(f.root, f.gh, models);
    assert.equal(result.config.model.modelId, 'test-model');
    assert.equal(faux.state.callCount, 0);
    await assert.rejects(stat(f.state), { code: 'ENOENT' });
    await assert.rejects(readFile(join(f.directory, 'github.json')), { code: 'ENOENT' });
  } finally { await f.cleanup(); }
});
test('global admission serializes an issue and the persisted deadline cancels its shell work', { timeout: 20_000 }, async () => {
  const f = await fixture(); const { models, faux } = scriptedModels();
  await writeFile(join(f.root, 'romp-v2.toml'), 'timeout_ms = 500\n' + await readFile(join(f.root, 'romp-v2.toml'), 'utf8'));
  faux.setResponses([fauxAssistantMessage(fauxToolCall('bash', { command: 'sleep 30' }), { stopReason: 'toolUse' })]);
  const runtime = await Runtime.open(f.state, f.gh, { models, width: 2 });
  try {
    const attempts = await Promise.allSettled([runtime.enqueue(f.root, 17), runtime.enqueue(f.root, 17)]);
    assert.equal(attempts.filter((r) => r.status === 'fulfilled').length, 1);
    const rejected = attempts.find((r) => r.status === 'rejected');
    assert.match(rejected?.status === 'rejected' ? String(rejected.reason) : '', /already running/);
    const job = Object.values((await runtime.state()).jobs)[0]!;
    await delay(600);
    await runtime.tick();
    assert.equal((await runtime.wait(job.id)).outcome, 'timeout');
    assert.equal((await f.gh.read()).creates, 0);
  } finally { await runtime.close(); await f.cleanup(); }
});
