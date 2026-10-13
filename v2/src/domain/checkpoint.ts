import { z } from 'zod';
import {
  ArtifactSchema,
  JobOutcomeSchema,
  ReviewSchema,
  VerificationSchema,
  recordId,
} from './model.js';

const common = z
  .object({
    round: z.number().int().nonnegative(),
    builder: recordId.nullable(),
    reviewer: recordId.nullable(),
    report: z.string(),
    artifact: ArtifactSchema.nullable(),
    verification: z.array(VerificationSchema),
    sha: z.string(),
    prUrl: z.string(),
    review: ReviewSchema.nullable(),
    outcome: z.union([JobOutcomeSchema, z.literal('')]),
    detail: z.string(),
  })
  .strict();
const committed = { artifact: ArtifactSchema, sha: z.string().min(1) };
const published = { ...committed, prUrl: z.string().min(1) };
export const CheckpointSchema = z.discriminatedUnion('phase', [
  common.extend({ phase: z.literal('prepare') }),
  common.extend({ phase: z.literal('build') }),
  common.extend({ phase: z.literal('building'), builder: recordId }),
  common.extend({ phase: z.literal('artifact'), builder: recordId }),
  common.extend({ phase: z.literal('commit'), artifact: ArtifactSchema }),
  common.extend({ phase: z.literal('verify'), ...committed }),
  common.extend({ phase: z.literal('publish'), ...committed }),
  common.extend({ phase: z.literal('review'), ...published }),
  common.extend({ phase: z.literal('reviewing'), ...published, reviewer: recordId }),
  common.extend({ phase: z.literal('decide'), ...published, review: ReviewSchema }),
  common.extend({ phase: z.literal('finish'), outcome: JobOutcomeSchema }),
]);
export type Checkpoint = z.infer<typeof CheckpointSchema>;
export type PhaseCheckpoint<P extends Checkpoint['phase']> = Extract<Checkpoint, { phase: P }>;
export function initialCheckpoint(): PhaseCheckpoint<'prepare'> {
  return {
    phase: 'prepare',
    round: 0,
    builder: null,
    reviewer: null,
    report: '',
    artifact: null,
    verification: [],
    sha: '',
    prUrl: '',
    review: null,
    outcome: '',
    detail: '',
  };
}
