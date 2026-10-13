import { z } from 'zod';

export const nonEmptyText = z.string().trim().min(1);
export const recordId = z.number().int().positive().max(Number.MAX_SAFE_INTEGER);
export const ModelSchema = z.object({ provider: nonEmptyText, modelId: nonEmptyText }).strict();
export const ConfigSchema = z
  .object({
    label: nonEmptyText,
    claimedLabel: nonEmptyText,
    blockedLabel: nonEmptyText,
    changesRequestedLabel: nonEmptyText,
    base: z.string(),
    verify: z.array(nonEmptyText).min(1),
    model: ModelSchema,
    reviewModel: ModelSchema,
    review: z.boolean(),
    maxFixRounds: z.number().int().nonnegative(),
    timeoutMs: recordId,
  })
  .strict();
export const IssueSchema = z
  .object({
    number: recordId,
    title: nonEmptyText,
    body: z.string(),
    url: nonEmptyText,
    labels: z.array(z.string()),
  })
  .strict();
export const FindingSchema = z
  .object({
    severity: z.enum(['blocking', 'nit']),
    file: nonEmptyText,
    line: recordId,
    description: nonEmptyText,
  })
  .strict();
export const ReviewSchema = z
  .object({
    verdict: z.enum(['approve', 'fix']),
    findings: z.array(FindingSchema),
  })
  .strict()
  .refine(
    (review) =>
      (review.verdict === 'fix') ===
      review.findings.some((finding) => finding.severity === 'blocking'),
    { message: 'Review verdict disagrees with blocking findings' },
  );
export const VerificationSchema = z
  .object({ command: nonEmptyText, output: z.string(), exitCode: z.number().int() })
  .strict();
export const ArtifactSchema = z
  .object({
    title: nonEmptyText,
    body: nonEmptyText,
    commit: z
      .string()
      .regex(
        /^(feat|fix|refactor|docs|test|chore|perf|build|ci|style)(\([^\n]+\))?!?: .+/,
        'PR artifact requires a conventional commit subject',
      ),
  })
  .strict();
export const JobInputSchema = z
  .object({
    id: nonEmptyText,
    root: nonEmptyText,
    repo: nonEmptyText,
    issue: IssueSchema,
    config: ConfigSchema,
    branch: nonEmptyText,
    worktree: nonEmptyText,
    baseSha: nonEmptyText,
    base: nonEmptyText,
  })
  .strict();
export const JobOutcomeSchema = z.enum([
  'done',
  'blocked',
  'red',
  'changes-requested',
  'no-changes',
  'failed',
  'cancelled',
  'timeout',
]);
export const PhaseSchema = z.enum([
  'prepare',
  'build',
  'building',
  'artifact',
  'commit',
  'verify',
  'publish',
  'review',
  'reviewing',
  'decide',
  'finish',
]);
export const JobEventSchema = z
  .object({
    at: z.number(),
    phase: PhaseSchema,
    round: z.number().int().nonnegative(),
    sha: z.string(),
    report: z.string(),
    verification: z.array(VerificationSchema),
    review: ReviewSchema.nullable(),
  })
  .strict();
export const JobSchema = JobInputSchema.extend({
  taskId: recordId,
  conversationId: recordId,
  phase: z.union([PhaseSchema, z.literal('finished')]),
  startedAt: z.number(),
  finishedAt: z.number(),
  outcome: z.union([JobOutcomeSchema, z.literal('')]),
  detail: z.string(),
  prUrl: z.string(),
  cancellation: z.enum(['cancelled', 'timeout', '']),
  events: z.array(JobEventSchema),
});
export const RepositorySchema = z
  .object({ root: nonEmptyText, repo: nonEmptyText, enabled: z.boolean(), error: z.string() })
  .strict();
export const StateSchema = z
  .object({
    jobs: z.record(z.string(), JobSchema),
    repositories: z.record(z.string(), RepositorySchema),
  })
  .strict();

export type Model = z.infer<typeof ModelSchema>;
export type Config = z.infer<typeof ConfigSchema>;
export type Issue = z.infer<typeof IssueSchema>;
export type Finding = z.infer<typeof FindingSchema>;
export type Review = z.infer<typeof ReviewSchema>;
export type Verification = z.infer<typeof VerificationSchema>;
export type Artifact = z.infer<typeof ArtifactSchema>;
export type JobInput = z.infer<typeof JobInputSchema>;
export type JobOutcome = z.infer<typeof JobOutcomeSchema>;
export type Phase = z.infer<typeof PhaseSchema>;
export type Job = z.infer<typeof JobSchema>;
export type Repository = z.infer<typeof RepositorySchema>;
export type JobState = z.infer<typeof StateSchema>;
