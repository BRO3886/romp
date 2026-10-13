import type { Artifact, JobInput, Review } from './domain.js';

export function builderPrompt(job: JobInput): string {
  return `Work issue #${job.issue.number} in ${job.repo}, in the current worktree on ${job.branch}.
Read the repository's AGENTS.md and applicable project instructions before editing.

Issue: ${job.issue.title}\n${job.issue.url}\n${job.issue.body}

GATE: Before editing, require testable acceptance criteria, an identified area to change,
and enough constraints to determine the requested behavior. Do not invent requirements.
If the issue lacks them, write the specific gap to .romp/blocked.md and stop without edits.

DONE: Meet every acceptance criterion and pass each command in this order:
${job.config.verify.map((c) => `- ${c}`).join('\n')}

PROVE IT: Run these commands and include their fresh results in your final report.
Do not weaken or skip tests. Do not change unrelated files.
Do not commit, push, create PRs, change labels, or manage worktrees. Romp owns these actions.

REPORT: Write .romp/pull-request.md with this exact frontmatter and a reviewer-facing description:
---
title: <plain English PR title>
commit: <conventional commit subject>
---
<what changed, why, and validation>
Do not add tool attribution or co-author trailers.`;
}
export function parseArtifact(content: string): Artifact {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n([\s\S]+)$/.exec(content.trim());
  if (!match) throw new Error('Missing or invalid .romp/pull-request.md');
  const lines = match[1]!.split(/\r?\n/);
  const field = (name: string) => lines.find((line) => line.startsWith(`${name}:`))?.slice(name.length + 1).trim() ?? '';
  const artifact = { title: field('title'), commit: field('commit'), body: match[2]!.trim() };
  if (!artifact.title || !artifact.body || !/^(feat|fix|refactor|docs|test|chore|perf|build|ci|style)(\([^\n]+\))?!?: .+/.test(artifact.commit)) {
    throw new Error('PR artifact requires a title, description, and conventional commit subject');
  }
  return artifact;
}
export function parseReview(content: string): Review {
  const value: unknown = JSON.parse(content);
  if (!value || typeof value !== 'object') throw new Error('Review must be a JSON object');
  const review = value as Record<string, unknown>;
  if (!['approve', 'fix'].includes(String(review.verdict)) || !Array.isArray(review.findings)) throw new Error('Invalid review verdict or findings');
  for (const item of review.findings) {
    if (!item || typeof item !== 'object') throw new Error('Invalid finding');
    const f = item as Record<string, unknown>;
    if (!['blocking', 'nit'].includes(String(f.severity)) || typeof f.file !== 'string' || !f.file ||
      !Number.isSafeInteger(f.line) || Number(f.line) < 1 || typeof f.description !== 'string' || !f.description.trim()) throw new Error('Invalid finding fields');
  }
  const result = review as unknown as Review;
  if ((result.verdict === 'fix') !== result.findings.some((f) => f.severity === 'blocking')) throw new Error('Review verdict disagrees with blocking findings');
  return result;
}
export function reviewComment(round: number, review: Review): string {
  return `Romp v2 review pass ${round + 1}: ${review.verdict}\n\n${review.findings.length ? review.findings.map((f) => `- ${f.severity}: ${f.file}:${f.line}: ${f.description}`).join('\n') : 'No findings.'}`;
}
