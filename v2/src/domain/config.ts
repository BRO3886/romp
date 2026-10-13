import { parse } from 'smol-toml';
import { z } from 'zod';
import { nonEmptyText, type Config } from './model.js';

const model = z.object({ provider: nonEmptyText, model: nonEmptyText }).strict();
const review = z
  .object({
    enabled: z.boolean({ error: 'review.enabled must be a boolean' }).default(true),
    provider: nonEmptyText.optional(),
    model: nonEmptyText.optional(),
    max_fix_rounds: z.number().int().nonnegative().default(2),
  })
  .strict();
const configFile = z
  .object({
    label: nonEmptyText.default('romp:v2'),
    claimed_label: nonEmptyText.default('romp:v2:claimed'),
    blocked_label: nonEmptyText.default('romp:v2:blocked'),
    changes_requested_label: nonEmptyText.default('romp:v2:changes-requested'),
    base: nonEmptyText.optional(),
    timeout_ms: z.number().int().positive().default(1_800_000),
    verify: z
      .object({
        commands: z.array(nonEmptyText).min(1, 'verify.commands must contain explicit commands'),
      })
      .strict(),
    agent: model,
    review: review.default({ enabled: true, max_fix_rounds: 2 }),
  })
  .strict();
export function parseConfig(source: string): Config {
  const parsed = configFile.safeParse(parse(source));
  if (!parsed.success) {
    const unknown = parsed.error.issues.find((issue) => issue.code === 'unrecognized_keys');
    if (unknown?.code === 'unrecognized_keys')
      throw new Error(`Unknown config key: ${unknown.keys.join(', ')}`);
    throw parsed.error;
  }
  const data = parsed.data;
  return {
    label: data.label,
    claimedLabel: data.claimed_label,
    blockedLabel: data.blocked_label,
    changesRequestedLabel: data.changes_requested_label,
    base: data.base ?? '',
    verify: data.verify.commands,
    model: { provider: data.agent.provider, modelId: data.agent.model },
    reviewModel: {
      provider: data.review.provider ?? data.agent.provider,
      modelId: data.review.model ?? data.agent.model,
    },
    review: data.review.enabled,
    maxFixRounds: data.review.max_fix_rounds,
    timeoutMs: data.timeout_ms,
  };
}
