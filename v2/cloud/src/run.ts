import { cloudModels } from './models.js';
import { DurableObject } from 'cloudflare:workers';
import type { Models } from '@earendil-works/pi-ai/models';
import { Harness, createRegistry, defineExtension } from '@earendil-works/pi-durable';
import { openDurableObjectSqliteStorage } from '@earendil-works/pi-durable/storage/sqlite/cloudflare';
import { deliveryTask } from '../../src/adapters/pi/delivery.js';
import { PiJobExecution } from '../../src/adapters/pi/job-execution.js';
import { State } from '../../src/adapters/pi/documents.js';
import { context } from '../../src/adapters/pi/context.js';
import type { GitHub } from '../../src/application/ports.js';
import type { Job, JobInput, JobOutcome } from '../../src/domain/model.js';
import { Submission, type Bindings } from './config.js';
import { GitHubApp } from './github.js';
import { CloudSandbox, SandboxWorkspace, type Sandbox } from './sandbox.js';
import { sandboxTools } from './tools.js';

type Record = {
  submission: Submission;
  state: 'running' | 'cleanup' | 'finished';
  startedAt: number;
  cancellation: boolean;
  job?: Job;
  error: string;
  outcome?: JobOutcome;
  detail?: string;
};
export class IssueRun extends DurableObject<Bindings> {
  private driving = false;
  private live: PiJobExecution | undefined;
  private sandbox: Sandbox | undefined;
  protected runtime: Promise<PiJobExecution> | undefined;
  constructor(ctx: DurableObjectState, env: Bindings) {
    super(ctx, env);
  }
  protected createSandbox(): Sandbox {
    return new CloudSandbox(this.ctx);
  }
  protected createGitHub(submission: Submission): GitHub & { token(): Promise<string> } {
    return new GitHubApp(this.env, submission);
  }
  protected createModels(): Models {
    return cloudModels(this.env);
  }
  async start(value: Submission) {
    const submission = Submission.parse(value);
    const existing = await this.ctx.storage.get<Record>('run');
    if (existing) {
      if (existing.submission.id !== submission.id) throw new Error('Run identity mismatch');
      if (existing.state !== 'finished') await this.ctx.storage.setAlarm(Date.now() + 100);
      return;
    }
    await this.ctx.storage.transaction(async (tx) => {
      if (await tx.get('run')) return;
      await tx.put('run', {
        submission,
        state: 'running',
        startedAt: Date.now(),
        cancellation: false,
        error: '',
      } satisfies Record);
      await tx.setAlarm(Date.now() + 100);
    });
  }
  async status() {
    const record = await this.ctx.storage.get<Record>('run');
    const job =
      record && this.live ? (await this.live.state()).jobs[record.submission.id] : record?.job;
    return record
      ? {
          id: record.submission.id,
          state: record.state,
          error: record.error,
          cancellation: record.cancellation,
          outcome: job?.outcome || record.outcome || '',
          detail: job?.detail || record.detail || '',
          job: job ?? null,
        }
      : { state: 'absent' as const };
  }
  async cancel() {
    await this.ctx.storage.transaction(async (tx) => {
      const record = await tx.get<Record>('run');
      if (!record || record.state === 'finished') return;
      record.cancellation = true;
      await tx.put('run', record);
      await tx.setAlarm(Date.now() + 100);
    });
  }
  override async alarm() {
    const record = await this.ctx.storage.get<Record>('run');
    if (!record || record.state === 'finished') return;
    await this.ctx.storage.setAlarm(Date.now() + 15_000);
    if (this.driving) {
      if (
        (record.cancellation ||
          Date.now() > record.startedAt + record.submission.repository.config.timeoutMs) &&
        this.runtime
      ) {
        const execution = await this.runtime;
        const job = (await execution.state()).jobs[record.submission.id];
        if (job && !job.outcome)
          await execution.cancel(job, record.cancellation ? 'cancelled' : 'timeout');
      }
      return;
    }
    this.ctx.waitUntil(this.drive());
  }
  private async open(record: Record) {
    const submission = record.submission;
    const sandbox = (this.sandbox ??= this.createSandbox());
    const github = this.createGitHub(submission);
    const workspace = new SandboxWorkspace(sandbox, () => github.token());
    const tools = sandboxTools(sandbox);
    const models = this.createModels();
    const selected = models.getModel(
      submission.repository.config.model.provider,
      submission.repository.config.model.modelId,
    );
    if (!selected || !(await models.getAuth(selected)))
      throw new Error('Configured model or authentication is unavailable');
    const registry = createRegistry();
    const task = deliveryTask(github, workspace, tools);
    registry.install(tools.builder);
    registry.install(tools.reviewer);
    registry.install(defineExtension({ name: 'romp.delivery', tasks: [task] }));
    const harness = await Harness.open(
      await openDurableObjectSqliteStorage(this.ctx.storage),
      {
        models,
        registry,
        settings: {
          extensions: [],
          toolExecution: 'sequential',
          stream: { timeoutMs: 120_000, maxRetries: 1 },
        },
      },
      context,
    );
    try {
      await harness.commit(async (tx) => {
        await tx.doc(State);
      }, context);
      const execution = new PiJobExecution(harness, task);
      if (!(await execution.state()).jobs[submission.id]) {
        let input = await this.ctx.storage.get<JobInput>('input');
        if (!input) {
          await github.preflight('/workspace');
          const cloned = await sandbox.exec(['bash', '-lc', 'test -d /workspace/repo/.git']);
          if (cloned.exitCode !== 0)
            await workspace.command(
              [
                'git',
                '-c',
                'core.hooksPath=/dev/null',
                'clone',
                '--',
                `https://github.com/${submission.repository.repo}.git`,
                '/workspace/repo',
              ],
              '/workspace',
              undefined,
              true,
            );
          const identity = await github.identity('/workspace/repo');
          const issue = await github.issue(submission.repository.repo, submission.issue.number);
          const base = submission.repository.config.base || identity.base;
          const baseSha = await workspace.base('/workspace/repo', base);
          input = {
            id: submission.id,
            root: '/workspace/repo',
            repo: submission.repository.repo,
            issue,
            config: submission.repository.config,
            branch: `codex/romp-cloud-${submission.id}`,
            worktree: '/workspace/job',
            base,
            baseSha,
          };
          await sandbox.checkpoint();
          await this.ctx.storage.put('input', input);
        }
        await execution.admit(input, 1, record.startedAt);
      }
      this.live = execution;
      return execution;
    } catch (error) {
      await harness.close(context);
      throw error;
    }
  }
  private async update(change: (record: Record) => void) {
    await this.ctx.storage.transaction(async (tx) => {
      const record = await tx.get<Record>('run');
      if (!record) throw new Error('Run record missing');
      change(record);
      await tx.put('run', record);
    });
  }
  private async drive() {
    this.driving = true;
    try {
      let record = await this.ctx.storage.get<Record>('run');
      if (!record || record.state === 'finished') return;
      if (record.state === 'running') {
        if (
          (record.cancellation ||
            Date.now() >= record.startedAt + record.submission.repository.config.timeoutMs) &&
          !(await this.ctx.storage.get('input'))
        )
          await this.update((r) => {
            r.state = 'cleanup';
            r.outcome = r.cancellation ? 'cancelled' : 'timeout';
          });
        else {
          const execution = await (this.runtime ??= this.open(record));
          await execution.reconcile(Date.now());
          const job = (await execution.state()).jobs[record.submission.id];
          if (!job) throw new Error('Job missing after admission');
          const fresh = await this.ctx.storage.get<Record>('run');
          if (
            !job.outcome &&
            (fresh?.cancellation || Date.now() > record.startedAt + job.config.timeoutMs)
          )
            await execution.cancel(job, fresh?.cancellation ? 'cancelled' : 'timeout');
          await execution.wait(job);
          await execution.reconcile(Date.now());
          const finished = (await execution.state()).jobs[record.submission.id];
          if (!finished?.outcome) throw new Error('Run ended without an outcome');
          await this.update((r) => {
            r.job = finished;
            r.outcome = finished.outcome || 'failed';
            r.detail = finished.detail;
            r.state = 'cleanup';
            r.error = '';
          });
        }
      }
      record = await this.ctx.storage.get<Record>('run');
      if (record?.state === 'cleanup') {
        if (this.runtime) {
          this.live = undefined;
          await (await this.runtime).close();
          this.runtime = undefined;
        }
        await (this.sandbox ??= this.createSandbox()).destroy();
        await this.update((r) => {
          r.state = 'finished';
          r.error = '';
        });
        await this.ctx.storage.deleteAlarm();
      }
    } catch (error) {
      this.live = undefined;
      if (this.runtime) {
        try {
          await (await this.runtime).close();
        } catch {
          /* A failed open already closes its harness. */
        }
      }
      this.runtime = undefined;
      await this.update((r) => {
        r.error = error instanceof Error ? error.message : 'Run failed';
      });
      const record = await this.ctx.storage.get<Record>('run');
      if (
        record?.state === 'running' &&
        Date.now() > record.startedAt + record.submission.repository.config.timeoutMs
      )
        await this.update((r) => {
          r.state = 'cleanup';
          r.outcome = 'timeout';
          r.detail = r.error;
        });
    } finally {
      this.driving = false;
    }
  }
}
