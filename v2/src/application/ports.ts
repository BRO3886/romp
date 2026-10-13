import type {
  Config,
  Issue,
  Job,
  JobInput,
  JobOutcome,
  JobState,
  Verification,
} from '../domain/model.js';

export type PullRequest = {
  url: string;
  body: string;
  baseRefName: string;
  state: 'OPEN' | 'CLOSED' | 'MERGED';
};
export interface GitHub {
  identity(root: string): Promise<{ repo: string; base: string }>;
  preflight(root: string): Promise<void>;
  issue(repo: string, number: number): Promise<Issue>;
  issues(repo: string, label: string): Promise<Issue[]>;
  claim(repo: string, number: number, config: Config, signal: AbortSignal): Promise<void>;
  release(
    repo: string,
    number: number,
    config: Config,
    outcome: JobOutcome,
    signal?: AbortSignal,
  ): Promise<void>;
  pullRequest(repo: string, branch: string, signal: AbortSignal): Promise<PullRequest | undefined>;
  createPullRequest(
    repo: string,
    branch: string,
    base: string,
    title: string,
    body: string,
    signal: AbortSignal,
  ): Promise<string>;
  comment(
    repo: string,
    target: string | number,
    body: string,
    marker: string,
    signal?: AbortSignal,
  ): Promise<void>;
}
export interface Workspace {
  base(root: string, base: string): Promise<string>;
  prepare(job: JobInput, signal: AbortSignal): Promise<void>;
  commit(job: JobInput, subject: string, signal: AbortSignal): Promise<string>;
  head(job: JobInput, signal: AbortSignal): Promise<string>;
  diff(job: JobInput, signal: AbortSignal): Promise<string>;
  clean(job: JobInput, signal: AbortSignal): Promise<void>;
  verify(job: JobInput, signal: AbortSignal): Promise<Verification[]>;
  push(job: JobInput, sha: string, signal: AbortSignal): Promise<void>;
  cleanup(job: JobInput, signal?: AbortSignal): Promise<void>;
  readArtifact(job: JobInput, name: 'blocked.md' | 'pull-request.md'): Promise<string>;
  removeArtifact(job: JobInput, name: 'pull-request.md'): Promise<void>;
}
export interface Repositories {
  root(directory: string): Promise<string>;
  config(root: string): Promise<Config>;
}
export interface ModelAccess {
  check(config: Config): Promise<void>;
}
export type JobLogs = { events: Job['events']; task?: unknown };
export interface JobExecution {
  state(): Promise<JobState>;
  admit(input: JobInput, width: number, startedAt: number): Promise<{ id: string; taskId: number }>;
  register(root: string, repo: string, enabled: boolean): Promise<void>;
  repositoryError(repo: string, error: string): Promise<void>;
  reconcile(now: number): Promise<void>;
  cancel(job: Job, reason: 'cancelled' | 'timeout'): Promise<void>;
  wait(job: Job): Promise<void>;
  logs(job: Job): Promise<JobLogs>;
  steer(job: Job, content: string, requestId: string): Promise<number>;
  close(): Promise<void>;
}
