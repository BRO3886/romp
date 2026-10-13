import {
  ArtifactSchema,
  ReviewSchema,
  type Artifact,
  type JobInput,
  type Review,
} from './model.js';

export function builderPrompt(job: JobInput): string {
  return `Work issue #${job.issue.number} in ${job.repo}, in the current worktree on ${job.branch}.
Read the repository's AGENTS.md and applicable project instructions before editing.

Issue: ${job.issue.title}\n${job.issue.url}\n${job.issue.body}

GATE: Before editing, require testable acceptance criteria, an identified area to change,
and enough constraints to determine the requested behavior. Do not invent requirements.
If the issue lacks them, write the specific gap to .romp/blocked.md and stop without edits.

DONE: Meet every acceptance criterion and pass each command in this order:
${job.config.verify.map((command) => `- ${command}`).join('\n')}

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
  const [, header = '', body = ''] = match;
  const lines = header.split(/\r?\n/);
  const field = (name: string) =>
    lines
      .find((line) => line.startsWith(`${name}:`))
      ?.slice(name.length + 1)
      .trim() ?? '';
  return ArtifactSchema.parse({
    title: field('title'),
    commit: field('commit'),
    body: body.trim(),
  });
}
export function parseReview(content: string): Review {
  const value: unknown = JSON.parse(content);
  return ReviewSchema.parse(value);
}
export function reviewComment(round: number, review: Review): string {
  return `Romp v2 review pass ${round + 1}: ${review.verdict}\n\n${review.findings.length ? review.findings.map((finding) => `- ${finding.severity}: ${finding.file}:${finding.line}: ${finding.description}`).join('\n') : 'No findings.'}`;
}
