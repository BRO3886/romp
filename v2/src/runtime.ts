import { randomUUID } from 'node:crypto';
import { mkdir, realpath } from 'node:fs/promises';
import { join } from 'node:path';
import { BACKGROUND_CONTEXT } from '@earendil-works/chord/context';
import { createModels, type Models } from '@earendil-works/pi-ai/models';
import { openaiProvider } from '@earendil-works/pi-ai/providers/openai';
import { anthropicProvider } from '@earendil-works/pi-ai/providers/anthropic';
import { defineExtension, createRegistry, Harness } from '@earendil-works/pi-durable';
import { openNodeSqliteStorage } from '@earendil-works/pi-durable/storage/sqlite/node';
import { loadConfig, repositoryRoot } from './config.js';
import { Role, State, message, type Config, type Job, type JobInput, type JobOutcome } from './domain.js';
import { Reviewer, executionEnvironment, supervisedBuilder } from './environment.js';
import { Git } from './git.js';
import type { GitHub } from './github.js';
import { deliveryTask, type Checkpoint } from './workflow.js';
import { FileCredentials } from './auth.js';
import { recoverShells } from './supervision.js';

export const context = BACKGROUND_CONTEXT;
export function providerModels(stateDir?: string) {
  const models = createModels(stateDir ? { credentials: new FileCredentials(stateDir) } : {});
  models.setProvider(openaiProvider()); models.setProvider(anthropicProvider());
  return models;
}
export async function preflight(root: string, gh: GitHub, models: Models) {
  root = await repositoryRoot(root);
  const config = await loadConfig(root);
  await gh.preflight(root);
  const identity = await gh.identity(root);
  await checkModels(config, models);
  return { root, config, ...identity };
}
async function checkModels(config: Config, models: Models) {
  for (const selected of [config.model, ...(config.review ? [config.reviewModel] : [])]) {
    const model = models.getModel(selected.provider, selected.modelId);
    if (!model) throw new Error(`Unknown model ${selected.provider}/${selected.modelId}; use models to list supported IDs`);
    const auth = await models.getAuth(model);
    if (!auth) throw new Error(`No authentication configured for ${selected.provider}`);
  }
}
export class Runtime {
  private nextPoll = 0;
  private constructor(public readonly harness: Harness, private readonly gh: GitHub, private readonly models: Models,
    private readonly stateDir: string, private readonly task: ReturnType<typeof deliveryTask>, private readonly width: number) {}
  static async open(stateDir: string, gh: GitHub, options: { models?: Models; width?: number } = {}) {
    const width = options.width ?? 1;
    if (!Number.isSafeInteger(width) || width < 1 || width > 32) throw new Error('Daemon width must be between 1 and 32');
    const models = options.models ?? providerModels(stateDir);
    const task = deliveryTask(gh);
    const registry = createRegistry();
    registry.install(supervisedBuilder(join(stateDir, 'executions'))); registry.install(Reviewer);
    registry.install(defineExtension({ name: 'romp.delivery', tasks: [task] }));
    await mkdir(stateDir, { recursive: true, mode: 0o700 });
    stateDir = await realpath(stateDir);
    await recoverShells(join(stateDir, 'executions'));
    const harness = await Harness.open(await openNodeSqliteStorage(join(stateDir, 'agent.sqlite')), {
      models, registry,
      env: async ({ cwd, conversationId, read }) => {
        if (!cwd) throw new Error('Agent working directory is missing');
        const role = await read.snapshot(Role, conversationId, context);
        if (!role) throw new Error('Agent execution role is missing');
        return executionEnvironment(cwd, role.readOnly);
      },
      settings: { extensions: [], toolExecution: 'sequential', stream: { timeoutMs: 120_000, maxRetries: 1 } },
      onReport: (error) => process.stderr.write(`Pi Durable: ${message(error)}\n`),
    }, context);
    const runtime = new Runtime(harness, gh, models, stateDir, task, width);
    await harness.commit(async (tx) => { await tx.doc(State); }, context);
    await runtime.reconcile();
    harness.resume();
    return runtime;
  }
  async state() { return (await this.harness.snapshot(State, context))!; }
  async enqueue(directory: string, number: number, watched = false) {
    if (!Number.isSafeInteger(number) || number < 1) throw new Error('Issue must be a positive integer');
    const resolved = await preflight(directory, this.gh, this.models);
    const issue = await this.gh.issue(resolved.repo, number);
    if (watched && !issue.labels.includes(resolved.config.label)) throw new Error('Issue no longer carries the trigger label');
    if (issue.labels.includes(resolved.config.claimedLabel)) throw new Error('Issue is already claimed');
    const base = resolved.config.base || resolved.base;
    const baseSha = await new Git().base(resolved.root, base);
    const id = randomUUID();
    const input: JobInput = {
      id, root: resolved.root, repo: resolved.repo, issue, config: resolved.config, base, baseSha,
      branch: `codex/romp-v2-${number}-${id.slice(0, 8)}`, worktree: join(this.stateDir, 'worktrees', id),
    };
    const taskId = await this.harness.commit(async (tx) => {
      const state = await tx.doc(State);
      const jobs = Object.values(state.jobs);
      if (jobs.filter((j) => !j.outcome).length >= this.width) throw new Error('Daemon has no available job capacity');
      if (jobs.some((j) => j.repo === input.repo && j.issue.number === number && !j.outcome)) throw new Error('Issue is already running');
      const conversation = await tx.createConversation({ ownership: { kind: 'ownerless' } });
      const taskId = await tx.createTask(this.task, input, { ownership: { kind: 'conversation' }, conversationId: conversation.id });
      state.jobs[id] = { ...input, taskId, conversationId: conversation.id, phase: 'prepare', startedAt: Date.now(),
        finishedAt: 0, outcome: '', detail: '', prUrl: '', cancellation: '', events: [] };
      return taskId;
    }, context);
    this.harness.resume();
    return { id, taskId };
  }
  async watch(directory: string, enabled: boolean) {
    const root = await repositoryRoot(directory);
    const resolved = enabled ? await preflight(root, this.gh, this.models) : { root, ...await this.gh.identity(root) };
    await this.harness.commit(async (tx) => {
      const state = await tx.doc(State);
      const existing = state.repositories[resolved.repo];
      if (existing && existing.root !== resolved.root) throw new Error('Origin is already registered at a different checkout');
      state.repositories[resolved.repo] = { root: resolved.root, repo: resolved.repo, enabled, error: '' };
    }, context);
    this.nextPoll = 0;
    return resolved.repo;
  }
  async tick() {
    await this.reconcile();
    const state = await this.state();
    for (const job of Object.values(state.jobs)) {
      if (!job.outcome && Date.now() >= job.startedAt + job.config.timeoutMs) await this.cancel(job.id, 'timeout');
    }
    if (Date.now() < this.nextPoll) return;
    this.nextPoll = Date.now() + 60_000;
    for (const repo of Object.values(state.repositories).filter((r) => r.enabled)) {
      if (Object.values((await this.state()).jobs).filter((j) => !j.outcome).length >= this.width) break;
      try {
        const config = await loadConfig(repo.root);
        const issues = await this.gh.issues(repo.repo, config.label);
        for (const issue of issues) {
          if (issue.labels.some((l) => [config.claimedLabel, config.blockedLabel, config.changesRequestedLabel].includes(l))) continue;
          if (Object.values((await this.state()).jobs).some((j) => j.repo === repo.repo && j.issue.number === issue.number && !j.outcome)) continue;
          await this.enqueue(repo.root, issue.number, true);
          break;
        }
        await this.harness.commit(async (tx) => { (await tx.doc(State)).repositories[repo.repo]!.error = ''; }, context);
      } catch (error) {
        await this.harness.commit(async (tx) => { (await tx.doc(State)).repositories[repo.repo]!.error = message(error); }, context);
      }
    }
  }
  async reconcile() {
    for (const job of Object.values((await this.state()).jobs)) {
      if (job.outcome) continue;
      const task = await this.harness.getTask(job.taskId, context);
      if (task?.state.status !== 'terminal') continue;
      const outcome = task.state.outcome;
      await this.harness.commit(async (tx) => {
        const record = (await tx.doc(State)).jobs[job.id]!;
        record.outcome = outcome.status === 'completed' ? outcome.result : outcome.status === 'aborted' ? outcome.result ?? 'cancelled' : 'failed';
        record.detail = 'error' in outcome && outcome.error ? outcome.error.message : record.detail;
        record.phase = 'finished'; record.finishedAt = Date.now();
      }, context);
    }
  }
  async job(id: string): Promise<Job> {
    await this.reconcile();
    const job = (await this.state()).jobs[id];
    if (!job) throw new Error(`Unknown job ${id}`);
    return job;
  }
  async cancel(id: string, reason: 'cancelled' | 'timeout' = 'cancelled') {
    const job = await this.job(id);
    if (job.outcome) return;
    await this.harness.commit(async (tx) => {
      const record = (await tx.doc(State)).jobs[id]!;
      if (!record.outcome) record.cancellation = record.cancellation || reason;
    }, context);
    await this.harness.abortTask(job.taskId, context);
  }
  async wait(id: string): Promise<Job> {
    const job = await this.job(id);
    await this.harness.waitForTask(job.taskId, context);
    return this.job(id);
  }
  async logs(id: string) {
    const job = await this.job(id);
    return { events: job.events, task: await this.harness.getTask(job.taskId, context) };
  }
  async steer(id: string, content: string) {
    if (!content.trim()) throw new Error('Steering message is empty');
    const job = await this.job(id);
    if (job.outcome) throw new Error('Job already finished');
    const task = await this.harness.getTask(job.taskId, context);
    if (!task || !('checkpoint' in task.state)) throw new Error('Job has no live checkpoint');
    const state = task.state.checkpoint as Checkpoint;
    if (state.phase !== 'building' || !state.builder) throw new Error('Only an active builder can be steered');
    const conversation = await this.harness.conversation(state.builder, context);
    if (!conversation) throw new Error('Builder conversation is missing');
    return (await conversation.submit({ type: 'input', content, whenBusy: 'steer', requestId: randomUUID() }, context)).id;
  }
  async close() { await this.harness.close(context); }
}
