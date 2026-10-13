import { context } from '../src/adapters/pi/context.js';
import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai/providers/faux';
import { openLocalRuntime } from '../src/composition/node.js';
import { executionEnvironment } from '../src/adapters/node/environment.js';
import { command } from '../src/adapters/node/process.js';
import { fixture, scriptedModels, buildResponses, approve } from './fixture.js';
import { artifact } from './fixture.js';

await test(
  'real Pi tools, SQLite, git, verification, review, and publication complete a job',
  { timeout: 20_000 },
  async () => {
    const f = await fixture();
    const { models, faux } = scriptedModels();
    faux.setResponses([...buildResponses(), approve()]);
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const queued = await runtime.enqueue(f.root, 17);
      const job = await runtime.wait(queued.id);
      assert.equal(job.outcome, 'done', job.detail);
      assert.equal((await f.gh.read()).creates, 1);
      assert.equal((await f.gh.read()).comments.length, 1);
      const published = await command('git', ['show', `${job.branch}:answer.txt`], f.remote);
      assert.equal(published.stdout, '42');
      const files = (await command('git', ['ls-tree', '-r', '--name-only', job.branch], f.remote))
        .stdout;
      assert(!files.includes('.romp/pull-request.md'));
      await assert.rejects(stat(job.worktree), { code: 'ENOENT' });
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
await test(
  'independent verification failure prevents publication and preserves the worktree',
  { timeout: 20_000 },
  async () => {
    const f = await fixture();
    const { models, faux } = scriptedModels();
    faux.setResponses(buildResponses('wrong'));
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait((await runtime.enqueue(f.root, 17)).id);
      assert.equal(job.outcome, 'red', job.detail);
      assert.equal((await f.gh.read()).creates, 0);
      assert.equal(await readFile(join(job.worktree, 'answer.txt'), 'utf8'), 'wrong');
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
await test(
  'blocking review invokes a repair and independently verifies the replacement commit',
  { timeout: 20_000 },
  async () => {
    const f = await fixture({ verify: 'node -e "process.exit(0)"' });
    const { models, faux } = scriptedModels();
    faux.setResponses([
      ...buildResponses('wrong'),
      fauxAssistantMessage(
        JSON.stringify({
          verdict: 'fix',
          findings: [
            {
              severity: 'blocking',
              file: 'answer.txt',
              line: 1,
              description: 'Acceptance requires 42.',
            },
          ],
        }),
      ),
      ...buildResponses(),
      approve(),
    ]);
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait((await runtime.enqueue(f.root, 17)).id);
      assert.equal(job.outcome, 'done', job.detail);
      assert.equal((await f.gh.read()).creates, 1);
      assert.equal((await f.gh.read()).comments.length, 2);
      assert.equal(
        (await command('git', ['show', `${job.branch}:answer.txt`], f.remote)).stdout,
        '42',
      );
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
await test(
  'reviewer mutation is denied by both the environment and Pi tool selection',
  { timeout: 20_000 },
  async () => {
    const f = await fixture();
    const env = executionEnvironment(f.root, true);
    await assert.rejects(async () => env.writeFile('answer.txt', 'mutated', context), /Read-only/);
    await assert.rejects(
      async () => env.exec('echo mutated > answer.txt', undefined, context),
      /Read-only/,
    );
    const { models, faux } = scriptedModels();
    faux.setResponses([
      ...buildResponses(),
      fauxAssistantMessage(fauxToolCall('write', { path: 'answer.txt', content: 'mutated' }), {
        stopReason: 'toolUse',
      }),
      approve(),
    ]);
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait((await runtime.enqueue(f.root, 17)).id);
      assert.equal(job.outcome, 'done', job.detail);
      assert.equal(
        (await command('git', ['show', `${job.branch}:answer.txt`], f.remote)).stdout,
        '42',
      );
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
await test(
  'invalid review output fails the job while preserving its published PR and worktree',
  { timeout: 20_000 },
  async () => {
    const f = await fixture();
    const { models, faux } = scriptedModels();
    faux.setResponses([...buildResponses(), fauxAssistantMessage('Looks fine')]);
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait((await runtime.enqueue(f.root, 17)).id);
      assert.equal(job.outcome, 'failed');
      assert.equal((await f.gh.read()).creates, 1);
      assert(job.prUrl);
      await stat(job.worktree);
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
await test(
  'exhausted repair budget preserves blocking findings and the rejected worktree',
  { timeout: 20_000 },
  async () => {
    const f = await fixture({ maxFixRounds: 0 });
    const { models, faux } = scriptedModels();
    faux.setResponses([
      ...buildResponses(),
      fauxAssistantMessage(
        JSON.stringify({
          verdict: 'fix',
          findings: [
            {
              severity: 'blocking',
              file: 'answer.txt',
              line: 1,
              description: 'A testable missing requirement.',
            },
          ],
        }),
      ),
    ]);
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait((await runtime.enqueue(f.root, 17)).id);
      assert.equal(job.outcome, 'changes-requested', job.detail);
      assert.equal(faux.state.callCount, 3);
      assert.equal(job.events.filter((e) => e.review?.verdict === 'fix').length, 1);
      await stat(job.worktree);
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
await test(
  'an artifact without source changes does not open an empty PR',
  { timeout: 20_000 },
  async () => {
    const f = await fixture();
    const { models, faux } = scriptedModels();
    faux.setResponses([
      fauxAssistantMessage(
        fauxToolCall('write', { path: '.romp/pull-request.md', content: artifact }),
        { stopReason: 'toolUse' },
      ),
      fauxAssistantMessage('No change required.'),
    ]);
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait((await runtime.enqueue(f.root, 17)).id);
      assert.equal(job.outcome, 'no-changes', job.detail);
      assert.equal((await f.gh.read()).creates, 0);
      await assert.rejects(stat(job.worktree), { code: 'ENOENT' });
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
