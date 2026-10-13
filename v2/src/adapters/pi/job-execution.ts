import type { Harness } from '@earendil-works/pi-durable';
import type { JobExecution } from '../../application/ports.js';
import { CheckpointSchema } from '../../domain/checkpoint.js';
import type { Job, JobInput } from '../../domain/model.js';
import { requireValue, requireEntry } from '../../shared/errors.js';
import { context } from './context.js';
import type { deliveryTask } from './delivery.js';
import { State } from './documents.js';
import { conversationId, taskId } from './ids.js';

export class PiJobExecution implements JobExecution {
  constructor(
    private readonly harness: Harness,
    private readonly task: ReturnType<typeof deliveryTask>,
  ) {}
  async state() {
    return requireValue(await this.harness.snapshot(State, context), 'Durable state is missing');
  }
  async admit(input: JobInput, width: number, startedAt: number) {
    const id = await this.harness.commit(async (tx) => {
      const state = await tx.doc(State);
      const jobs = Object.values(state.jobs);
      if (jobs.filter((job) => !job.outcome).length >= width)
        throw new Error('Daemon has no available job capacity');
      if (
        jobs.some(
          (job) =>
            job.repo === input.repo && job.issue.number === input.issue.number && !job.outcome,
        )
      )
        throw new Error('Issue is already running');
      const conversation = await tx.createConversation({ ownership: { kind: 'ownerless' } });
      const id = await tx.createTask(this.task, input, {
        ownership: { kind: 'conversation' },
        conversationId: conversation.id,
      });
      state.jobs[input.id] = {
        ...input,
        taskId: id,
        conversationId: conversation.id,
        phase: 'prepare',
        startedAt,
        finishedAt: 0,
        outcome: '',
        detail: '',
        prUrl: '',
        cancellation: '',
        events: [],
      };
      return id;
    }, context);
    this.harness.resume();
    return { id: input.id, taskId: id };
  }
  async register(root: string, repo: string, enabled: boolean) {
    await this.harness.commit(async (tx) => {
      const state = await tx.doc(State);
      const existing = state.repositories[repo];
      if (existing && existing.root !== root)
        throw new Error('Origin is already registered at a different checkout');
      state.repositories[repo] = { root, repo, enabled, error: '' };
    }, context);
  }
  async repositoryError(repo: string, error: string) {
    await this.harness.commit(async (tx) => {
      requireEntry(
        (await tx.doc(State)).repositories,
        repo,
        'Repository registration is missing',
      ).error = error;
    }, context);
  }
  async reconcile(now: number) {
    for (const job of Object.values((await this.state()).jobs)) {
      if (job.outcome) continue;
      const task = await this.harness.getTask(taskId(job.taskId), context);
      if (task?.state.status !== 'terminal') continue;
      const outcome = task.state.outcome;
      await this.harness.commit(async (tx) => {
        const record = requireEntry(
          (await tx.doc(State)).jobs,
          job.id,
          'Durable job record is missing',
        );
        if (outcome.status === 'completed') record.outcome = outcome.result;
        else if (outcome.status === 'aborted') record.outcome = outcome.result ?? 'cancelled';
        else record.outcome = 'failed';
        if ('error' in outcome && outcome.error) record.detail = outcome.error.message;
        record.phase = 'finished';
        record.finishedAt = now;
      }, context);
    }
  }
  async cancel(job: Job, reason: 'cancelled' | 'timeout') {
    await this.harness.commit(async (tx) => {
      const record = requireEntry(
        (await tx.doc(State)).jobs,
        job.id,
        'Durable job record is missing',
      );
      if (!record.outcome) record.cancellation ||= reason;
    }, context);
    await this.harness.abortTask(taskId(job.taskId), context);
  }
  async wait(job: Job) {
    await this.harness.waitForTask(taskId(job.taskId), context);
  }
  async logs(job: Job) {
    return { events: job.events, task: await this.harness.getTask(taskId(job.taskId), context) };
  }
  async steer(job: Job, content: string, requestId: string) {
    const task = await this.harness.getTask(taskId(job.taskId), context);
    if (!task || !('checkpoint' in task.state)) throw new Error('Job has no live checkpoint');
    const state = CheckpointSchema.parse(task.state.checkpoint);
    if (state.phase !== 'building') throw new Error('Only an active builder can be steered');
    const conversation = requireValue(
      await this.harness.conversation(conversationId(state.builder), context),
      'Builder conversation is missing',
    );
    return (
      await conversation.submit({ type: 'input', content, whenBusy: 'steer', requestId }, context)
    ).id;
  }
  async close() {
    await this.harness.close(context);
  }
}
