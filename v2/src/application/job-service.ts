import type { Job, JobInput } from '../domain/model.js';
import { message, requireEntry } from '../shared/errors.js';
import type { GitHub, JobExecution, ModelAccess, Repositories, Workspace } from './ports.js';
import { preflight } from './preflight.js';

export type JobDependencies = Readonly<{
  execution: JobExecution;
  github: GitHub;
  models: ModelAccess;
  repositories: Repositories;
  workspace: Workspace;
  worktreePath: (id: string) => string;
  newId: () => string;
  now: () => number;
  width: number;
}>;
export class JobService {
  private nextPoll = 0;
  constructor(private readonly dependencies: JobDependencies) {}
  state() {
    return this.dependencies.execution.state();
  }
  async enqueue(directory: string, number: number, watched = false) {
    if (!Number.isSafeInteger(number) || number < 1)
      throw new Error('Issue must be a positive integer');
    const d = this.dependencies;
    const resolved = await preflight(directory, d);
    const issue = await d.github.issue(resolved.repo, number);
    if (watched && !issue.labels.includes(resolved.config.label))
      throw new Error('Issue no longer carries the trigger label');
    if (issue.labels.includes(resolved.config.claimedLabel))
      throw new Error('Issue is already claimed');
    const base = resolved.config.base || resolved.base;
    const baseSha = await d.workspace.base(resolved.root, base);
    const id = d.newId();
    const input: JobInput = {
      id,
      root: resolved.root,
      repo: resolved.repo,
      issue,
      config: resolved.config,
      base,
      baseSha,
      branch: `codex/romp-v2-${number}-${id.slice(0, 8)}`,
      worktree: d.worktreePath(id),
    };
    return d.execution.admit(input, d.width, d.now());
  }
  async watch(directory: string, enabled: boolean) {
    const d = this.dependencies;
    const root = await d.repositories.root(directory);
    const resolved = enabled
      ? await preflight(root, d)
      : { root, ...(await d.github.identity(root)) };
    await d.execution.register(resolved.root, resolved.repo, enabled);
    this.nextPoll = 0;
    return resolved.repo;
  }
  async tick() {
    await this.reconcile();
    const d = this.dependencies;
    for (const job of Object.values((await this.state()).jobs)) {
      if (!job.outcome && d.now() >= job.startedAt + job.config.timeoutMs)
        await this.cancel(job.id, 'timeout');
    }
    if (d.now() < this.nextPoll) return;
    this.nextPoll = d.now() + 60_000;
    for (const repo of Object.values((await this.state()).repositories).filter((r) => r.enabled)) {
      if (Object.values((await this.state()).jobs).filter((j) => !j.outcome).length >= d.width)
        break;
      try {
        const config = await d.repositories.config(repo.root);
        for (const issue of await d.github.issues(repo.repo, config.label)) {
          if (
            issue.labels.some((label) =>
              [config.claimedLabel, config.blockedLabel, config.changesRequestedLabel].includes(
                label,
              ),
            )
          )
            continue;
          if (
            Object.values((await this.state()).jobs).some(
              (job) => job.repo === repo.repo && job.issue.number === issue.number && !job.outcome,
            )
          )
            continue;
          await this.enqueue(repo.root, issue.number, true);
          break;
        }
        await d.execution.repositoryError(repo.repo, '');
      } catch (error) {
        await d.execution.repositoryError(repo.repo, message(error));
      }
    }
  }
  reconcile() {
    return this.dependencies.execution.reconcile(this.dependencies.now());
  }
  async job(id: string): Promise<Job> {
    await this.reconcile();
    return requireEntry((await this.state()).jobs, id, `Unknown job ${id}`);
  }
  async cancel(id: string, reason: 'cancelled' | 'timeout' = 'cancelled') {
    const job = await this.job(id);
    if (!job.outcome) await this.dependencies.execution.cancel(job, reason);
  }
  async wait(id: string): Promise<Job> {
    await this.dependencies.execution.wait(await this.job(id));
    return this.job(id);
  }
  async logs(id: string) {
    return this.dependencies.execution.logs(await this.job(id));
  }
  async steer(id: string, content: string) {
    if (!content.trim()) throw new Error('Steering message is empty');
    const job = await this.job(id);
    if (job.outcome) throw new Error('Job already finished');
    return this.dependencies.execution.steer(job, content, this.dependencies.newId());
  }
  close() {
    return this.dependencies.execution.close();
  }
}
