import { readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { parse } from 'smol-toml';
import { command } from './process.js';
import type { Config, Model } from './domain.js';

function table(value: unknown, name: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${name} must be a table`);
  return value as Record<string, unknown>;
}
function keys(value: Record<string, unknown>, allowed: string[], name: string) {
  for (const key of Object.keys(value)) if (!allowed.includes(key)) throw new Error(`Unknown ${name}.${key}`);
}
function text(value: unknown, name: string): string {
  if (typeof value !== 'string' || !value.trim()) throw new Error(`${name} must be a non-empty string`);
  return value.trim();
}
function integer(value: unknown, name: string, minimum: number): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < minimum) throw new Error(`${name} must be an integer >= ${minimum}`);
  return value;
}
function model(value: unknown, name: string): Model {
  const data = table(value, name);
  keys(data, ['provider', 'model'], name);
  return { provider: text(data.provider, `${name}.provider`), modelId: text(data.model, `${name}.model`) };
}
export function parseConfig(source: string): Config {
  const data = table(parse(source), 'config');
  keys(data, ['label', 'claimed_label', 'blocked_label', 'changes_requested_label', 'base', 'timeout_ms', 'verify', 'agent', 'review'], 'config');
  const verify = table(data.verify, 'verify');
  keys(verify, ['commands'], 'verify');
  if (!Array.isArray(verify.commands) || !verify.commands.length) throw new Error('verify.commands must contain explicit commands');
  const agent = model(data.agent, 'agent');
  const review = data.review === undefined ? {} : table(data.review, 'review');
  keys(review, ['enabled', 'provider', 'model', 'max_fix_rounds'], 'review');
  if (review.enabled !== undefined && typeof review.enabled !== 'boolean') throw new Error('review.enabled must be a boolean');
  return {
    label: text(data.label ?? 'romp:v2', 'label'), claimedLabel: text(data.claimed_label ?? 'romp:v2:claimed', 'claimed_label'),
    blockedLabel: text(data.blocked_label ?? 'romp:v2:blocked', 'blocked_label'),
    changesRequestedLabel: text(data.changes_requested_label ?? 'romp:v2:changes-requested', 'changes_requested_label'),
    base: data.base === undefined ? '' : text(data.base, 'base'),
    verify: verify.commands.map((v) => text(v, 'verify.commands')),
    model: agent,
    reviewModel: model({ provider: review.provider ?? agent.provider, model: review.model ?? agent.modelId }, 'review'),
    review: review.enabled !== false, maxFixRounds: integer(review.max_fix_rounds ?? 2, 'review.max_fix_rounds', 0),
    timeoutMs: integer(data.timeout_ms ?? 1_800_000, 'timeout_ms', 1),
  };
}
export async function loadConfig(root: string): Promise<Config> {
  return parseConfig(await readFile(join(root, 'romp-v2.toml'), 'utf8'));
}
export async function repositoryRoot(directory: string): Promise<string> {
  return resolve((await command('git', ['rev-parse', '--show-toplevel'], directory)).stdout.trim());
}
