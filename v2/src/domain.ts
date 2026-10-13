import { defineDoc, type ConversationId, type TaskId } from '@earendil-works/pi-durable';

export type Model = { provider: string; modelId: string };
export type Config = {
  label: string;
  claimedLabel: string;
  blockedLabel: string;
  changesRequestedLabel: string;
  base: string;
  verify: string[];
  model: Model;
  reviewModel: Model;
  review: boolean;
  maxFixRounds: number;
  timeoutMs: number;
};
export type Issue = { number: number; title: string; body: string; url: string; labels: string[] };
export type Finding = { severity: 'blocking' | 'nit'; file: string; line: number; description: string };
export type Review = { verdict: 'approve' | 'fix'; findings: Finding[] };
export type Verification = { command: string; output: string; exitCode: number };
export type Artifact = { title: string; commit: string; body: string };
export type JobInput = {
  id: string;
  root: string;
  repo: string;
  issue: Issue;
  config: Config;
  branch: string;
  worktree: string;
  baseSha: string;
  base: string;
};
export type JobOutcome = 'done' | 'blocked' | 'red' | 'changes-requested' | 'no-changes' | 'failed' | 'cancelled' | 'timeout';
export type Job = JobInput & {
  taskId: TaskId<JobOutcome>;
  conversationId: ConversationId;
  phase: string;
  startedAt: number;
  finishedAt: number;
  outcome: JobOutcome | '';
  detail: string;
  prUrl: string;
  cancellation: 'cancelled' | 'timeout' | '';
  events: { at: number; phase: string; round: number; sha: string; report: string; verification: Verification[]; review: Review | null }[];
};
export type Repository = { root: string; repo: string; enabled: boolean; error: string };
export const State = defineDoc<{ jobs: Record<string, Job>; repositories: Record<string, Repository> }>({
  kind: 'romp.state', version: 1, scope: 'session',
  initial: () => ({ jobs: {}, repositories: {} }),
});
export const Role = defineDoc<{ readOnly: boolean }>({
  kind: 'romp.role', version: 1, scope: 'conversation', history: 'latest', fork: 'initial',
  initial: () => ({ readOnly: false }),
});
export function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
