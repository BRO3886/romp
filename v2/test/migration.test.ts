import assert from 'node:assert/strict';
import test from 'node:test';
import { writeFile, mkdir, realpath, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { createRegistry, defineExtension, defineTask, Harness } from '@earendil-works/pi-durable';
import { openNodeSqliteStorage } from '@earendil-works/pi-durable/storage/sqlite/node';
import type { JobInput, JobOutcome } from '../src/domain/model.js';
import { initialCheckpoint, type Checkpoint } from '../src/domain/checkpoint.js';
import { State } from '../src/adapters/pi/documents.js';
import { context } from '../src/adapters/pi/context.js';
import { openLocalRuntime } from '../src/composition/node.js';
import { LocalRepositories } from '../src/adapters/node/repositories.js';
import { Git } from '../src/adapters/node/git.js';
import { z } from 'zod';
import { fixture, scriptedModels } from './fixture.js';

await test(
  'real SQLite reopens a version-one delivery checkpoint and completes under version two',
  { timeout: 20_000 },
  async () => {
    const f = await fixture({ review: false });
    const { models, faux } = scriptedModels();
    const repository = new LocalRepositories();
    const workspace = new Git();
    await mkdir(f.state, { recursive: true });
    const stateDirectory = await realpath(f.state);
    const input: JobInput = {
      id: 'migration-job',
      root: await repository.root(f.root),
      repo: 'test/repo',
      issue: await f.gh.issue('test/repo', 17),
      config: await repository.config(f.root),
      branch: 'codex/migration-test',
      worktree: join(stateDirectory, 'worktrees', 'migration-job'),
      base: 'main',
      baseSha: await workspace.base(f.root, 'main'),
    };
    await workspace.prepare(input, new AbortController().signal);
    await writeFile(join(input.worktree, 'answer.txt'), '42');
    const sha = await workspace.commit(
      input,
      'fix: write the answer',
      new AbortController().signal,
    );
    const checkpoint: Checkpoint = {
      ...initialCheckpoint(),
      phase: 'publish',
      sha,
      artifact: {
        title: 'Write the answer',
        commit: 'fix: write the answer',
        body: 'Set answer.txt to 42.',
      },
    };
    const legacy = defineTask<JobInput, { phase: 'publish' }, JobOutcome>({
      name: 'romp.delivery',
      version: 1,
      initial: () => checkpoint,
      phases: { publish: () => Promise.reject(new Error('Legacy task must remain pending')) },
      abort: () => Promise.reject(new Error('Legacy task must not abort')),
    });
    const registry = createRegistry();
    registry.install(defineExtension({ name: 'romp.delivery', tasks: [legacy] }));
    const harness = await Harness.open(
      await openNodeSqliteStorage(join(f.state, 'agent.sqlite')),
      { models, registry },
      context,
    );
    try {
      await harness.commit(async (tx) => {
        const state = await tx.doc(State);
        const conversation = await tx.createConversation({ ownership: { kind: 'ownerless' } });
        const id = await tx.createTask(legacy, input, {
          ownership: { kind: 'conversation' },
          conversationId: conversation.id,
        });
        state.jobs[input.id] = {
          ...input,
          taskId: id,
          conversationId: conversation.id,
          phase: 'publish',
          startedAt: Date.now(),
          finishedAt: 0,
          outcome: '',
          detail: '',
          prUrl: '',
          cancellation: '',
          events: [],
        };
      }, context);
    } finally {
      await harness.close(context);
    }
    const runtime = await openLocalRuntime(f.state, f.gh, { models });
    try {
      const job = await runtime.wait(input.id);
      assert.equal(job.outcome, 'done', job.detail);
      assert.equal((await f.gh.read()).creates, 1);
      assert.equal(faux.state.callCount, 0);
      const receipt = z
        .object({ task: z.object({ version: z.number() }) })
        .parse(await runtime.logs(job.id));
      assert.equal(receipt.task?.version, 2);
      await assert.rejects(stat(job.worktree), { code: 'ENOENT' });
    } finally {
      await runtime.close();
      await f.cleanup();
    }
  },
);
