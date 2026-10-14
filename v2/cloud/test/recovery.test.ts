import { env } from 'cloudflare:workers';
import {
  evictDurableObject,
  runDurableObjectAlarm,
  runInDurableObject,
  reset,
} from 'cloudflare:test';
import { afterEach, expect, test } from 'vitest';
import { IssueRun as TestRun } from './worker.js';
import { submission } from './fixture.js';
afterEach(async () => {
  await reset();
});
async function until(check: () => Promise<boolean>, timeout = 30_000) {
  const end = Date.now() + timeout;
  while (!(await check())) {
    if (Date.now() > end) throw new Error('Condition timed out');
    await new Promise((r) => setTimeout(r, 25));
  }
}
test('Pi on Workers SQLite builds, verifies, publishes, and destroys a real Linux workspace', async () => {
  const input = submission(21);
  const run = env.RUNS.getByName(input.id);
  await run.start(input);
  await runDurableObjectAlarm(run);
  await until(async () => (await run.status()).state === 'finished');
  const result = await run.status();
  expect('job' in result && result.job?.outcome, JSON.stringify(result)).toBe('done');
  expect('job' in result && result.job?.prUrl).toBe('https://github.com/test/repo/pull/121');
  await runInDurableObject(run, async (instance, ctx) => {
    if (!(instance instanceof TestRun)) throw new Error('Wrong test host');
    expect((await instance.evidence())?.creates).toBe(1);
    expect(await ctx.storage.get('model-calls')).toBe(2);
  });
});
test('PR creation interrupted before its checkpoint resumes without a duplicate PR or builder run', async () => {
  const input = submission(22);
  let run = env.RUNS.getByName(input.id);
  await runInDurableObject(run, (instance) => {
    if (!(instance instanceof TestRun)) throw new Error('Wrong test host');
    instance.fault(true);
  });
  await run.start(input);
  await runDurableObjectAlarm(run);
  await until(async () =>
    runInDurableObject(run, async (instance) => {
      if (!(instance instanceof TestRun)) throw new Error('Wrong test host');
      return (await instance.evidence())?.creates === 1;
    }),
  );
  await expect(
    runInDurableObject(run, async (instance, ctx) => {
      if (!(instance instanceof TestRun)) throw new Error('Wrong test host');
      await instance.destroyWorkspace();
      ctx.abort('Injected actor crash after PR creation');
    }),
  ).rejects.toThrow('Injected actor crash');
  run = env.RUNS.getByName(input.id);
  await run.start(input);
  await runDurableObjectAlarm(run);
  await until(async () => (await run.status()).state === 'finished');
  const result = await run.status();
  expect('job' in result && result.job?.outcome, JSON.stringify(result)).toBe('done');
  await runInDurableObject(run, async (instance, ctx) => {
    if (!(instance instanceof TestRun)) throw new Error('Wrong test host');
    expect((await instance.evidence())?.creates).toBe(1);
    expect(await ctx.storage.get('model-calls')).toBe(2);
  });
});
test('cleanup failure retains the scheduler slot, survives eviction, and retries destruction', async () => {
  const input = submission(23);
  const run = env.RUNS.getByName(input.id);
  await runInDurableObject(run, async (_instance, ctx) => {
    await ctx.storage.put('run', {
      submission: input,
      state: 'cleanup',
      startedAt: Date.now(),
      cancellation: false,
      error: '',
    });
    await ctx.storage.put('fail-destroy', true);
  });
  const scheduler = env.SCHEDULER.getByName('installation');
  await scheduler.enqueue(input);
  await scheduler.enqueue(submission(24));
  await runInDurableObject(scheduler, (_instance, ctx) => {
    ctx.storage.sql.exec("UPDATE queue SET state='active' WHERE id=?", input.id);
  });
  await run.start(input);
  await runDurableObjectAlarm(run);
  await until(async () => {
    const s = await run.status();
    return 'error' in s && s.error === 'Injected destroy failure';
  });
  expect((await scheduler.status()).runs.filter((r) => r.state === 'active')).toHaveLength(1);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'queued')).toHaveLength(1);
  await evictDurableObject(run);
  await run.start(input);
  await runDurableObjectAlarm(run);
  await until(async () => (await run.status()).state === 'finished');
});
test('cancel before admission avoids model work and ends after sandbox destruction', async () => {
  const run = env.RUNS.getByName(submission(25).id);
  await run.start(submission(25));
  await run.cancel();
  await runDurableObjectAlarm(run);
  await until(async () => (await run.status()).state === 'finished');
  await runInDurableObject(run, async (_instance, ctx) => {
    expect(await ctx.storage.get('input')).toBeUndefined();
  });
});
test('scheduler enforces shared width across live sandboxes and recovers reserved slots after eviction', async () => {
  const inputs = [submission(31), submission(32), submission(33)];
  for (const input of inputs)
    await runInDurableObject(env.RUNS.getByName(input.id), (instance) => {
      if (!(instance instanceof TestRun)) throw new Error('Wrong host');
      instance.fault(true);
    });
  const scheduler = env.SCHEDULER.getByName('installation');
  await scheduler.configure(2);
  for (const input of inputs) await scheduler.enqueue(input);
  await runDurableObjectAlarm(scheduler);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'active')).toHaveLength(2);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'queued')).toHaveLength(1);
  for (const input of inputs.slice(0, 2)) await runDurableObjectAlarm(env.RUNS.getByName(input.id));
  await until(async () => {
    let count = 0;
    for (const input of inputs.slice(0, 2))
      count += await runInDurableObject(env.RUNS.getByName(input.id), async (instance) => {
        if (!(instance instanceof TestRun)) throw new Error('Wrong host');
        return (await instance.evidence())?.creates ?? 0;
      });
    return count === 2;
  });
  await scheduler.configure(1);
  await evictDurableObject(scheduler);
  await runDurableObjectAlarm(scheduler);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'active')).toHaveLength(2);
  for (const input of inputs.slice(0, 2)) {
    const run = env.RUNS.getByName(input.id);
    await runInDurableObject(run, async (instance) => {
      if (!(instance instanceof TestRun)) throw new Error('Wrong host');
      await instance.interrupt();
    });
    await evictDurableObject(run);
    await run.start(input);
    await runDurableObjectAlarm(run);
    await until(async () => (await run.status()).state === 'finished');
  }
  await runDurableObjectAlarm(scheduler);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'active')).toHaveLength(1);
  const last = inputs[2];
  if (!last) throw new Error('Missing input');
  const run = env.RUNS.getByName(last.id);
  await runInDurableObject(run, (instance) => {
    if (!(instance instanceof TestRun)) throw new Error('Wrong host');
    instance.fault(false);
  });
  await runDurableObjectAlarm(run);
  await until(async () => (await run.status()).state === 'finished');
  await runDurableObjectAlarm(scheduler);
  expect((await scheduler.status()).runs.filter((r) => r.state === 'active')).toHaveLength(0);
});
test('expired admission stops before model or workspace provisioning', async () => {
  const input = submission(41);
  const run = env.RUNS.getByName(input.id);
  await run.start(input);
  await runInDurableObject(run, async (_instance, ctx) => {
    const record = await ctx.storage.get<{ startedAt: number }>('run');
    if (!record) throw new Error('Missing run');
    record.startedAt = Date.now() - input.repository.config.timeoutMs - 1;
    await ctx.storage.put('run', record);
  });
  await runDurableObjectAlarm(run);
  await until(async () => (await run.status()).state === 'finished');
  const status = await run.status();
  expect('outcome' in status && status.outcome).toBe('timeout');
  await runInDurableObject(run, async (_instance, ctx) => {
    expect(await ctx.storage.get('input')).toBeUndefined();
    expect(await ctx.storage.get('model-calls')).toBeUndefined();
  });
});
