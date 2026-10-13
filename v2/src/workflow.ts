import { readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import type { Context } from '@earendil-works/chord';
import { AssistantEntry, configure, defineTask, type ConversationId, type TaskRuntime, type TypedEntry } from '@earendil-works/pi-durable';
import { State, Role, message, type Artifact, type JobInput, type JobOutcome, type Review, type Verification } from './domain.js';
import { builderPrompt, parseArtifact, parseReview, reviewComment } from './contracts.js';
import { Builder, Reviewer } from './environment.js';
import type { GitHub } from './github.js';
import { Git } from './git.js';

type Phase = 'prepare' | 'build' | 'building' | 'artifact' | 'commit' | 'verify' | 'publish' | 'review' | 'reviewing' | 'decide' | 'finish';
export type Checkpoint = {
  phase: Phase;
  round: number;
  builder: ConversationId | null;
  reviewer: ConversationId | null;
  report: string;
  artifact: Artifact | null;
  verification: Verification[];
  sha: string;
  prUrl: string;
  review: Review | null;
  outcome: JobOutcome | '';
  detail: string;
};
type Runtime = TaskRuntime<JobInput, Checkpoint, JobOutcome, object>;
type Handler = (input: JobInput, state: Checkpoint, runtime: Runtime, context: Context) => Promise<void>;

async function advance(input: JobInput, state: Checkpoint, runtime: Runtime, context: Context) {
  await runtime.commit(async (tx) => {
    const doc = await tx.doc(State);
    const job = doc.jobs[input.id];
    if (!job) throw new Error('Durable job record is missing');
    job.phase = state.phase;
    job.prUrl = state.prUrl;
    job.detail = state.detail;
    if (['artifact', 'publish', 'decide', 'finish'].includes(state.phase)) {
      job.events.push({ at: runtime.now(), phase: state.phase, round: state.round, sha: state.sha,
        report: state.phase === 'artifact' ? state.report : '', verification: state.phase === 'publish' || state.phase === 'finish' ? state.verification : [],
        review: state.phase === 'decide' ? state.review : null });
    }
    return { status: 'running', checkpoint: state };
  }, context);
}
async function optionalFile(path: string): Promise<string> {
  try { return await readFile(path, 'utf8'); }
  catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return ''; throw error; }
}
async function answer(id: ConversationId, prompt: string, requestId: string, runtime: Runtime, context: Context): Promise<string> {
  const conversation = await runtime.conversation(id, context);
  if (!conversation) throw new Error('Agent conversation is missing');
  const settled = await (await conversation.submit({ type: 'input', content: prompt, requestId }, context)).wait(context);
  if (settled.status !== 'done' || settled.type !== 'input') throw new Error(`Agent input ended ${settled.status}`);
  let entry: TypedEntry<never> | undefined;
  await runtime.commit(async (tx) => { entry = await tx.entry(AssistantEntry, settled.answer); return undefined; }, context);
  const output = entry?.model?.flatMap((m) => m.role === 'assistant' ? m.content.flatMap((c) => c.type === 'text' ? [c.text] : []) : []).join('\n') ?? '';
  if (!output.trim()) throw new Error('Agent completed without a final text report');
  return output;
}
export function deliveryTask(gh: GitHub, git = new Git()) {
  const startAgent = (readOnly: boolean): Handler => async (input, state, runtime, context) => {
    const field = readOnly ? 'reviewer' : 'builder';
    const phase = readOnly ? 'reviewing' : 'building';
    if (state[field]) { await advance(input, { ...state, phase }, runtime, context); return; }
    await runtime.commit(async (tx) => {
      const doc = await tx.doc(State);
      const conversation = await tx.createConversation({ ownership: { kind: 'task', taskId: runtime.taskId } });
      await configure(tx, conversation.id, {
        model: readOnly ? input.config.reviewModel : input.config.model,
        cwd: input.worktree, extensions: [readOnly ? Reviewer : Builder],
      });
      (await tx.doc(Role, conversation.id)).readOnly = readOnly;
      doc.jobs[input.id]!.phase = phase;
      return { status: 'running', checkpoint: { ...state, [field]: conversation.id, phase } };
    }, context);
  };
  const handlers: Record<Phase, Handler> = {
    prepare: async (input, state, runtime, context) => {
      await gh.claim(input.repo, input.issue.number, input.config, runtime.signal);
      await git.prepare(input, runtime.signal);
      await advance(input, { ...state, phase: 'build' }, runtime, context);
    },
    build: startAgent(false),
    building: async (input, state, runtime, context) => {
      const repair = state.round ? `\n\nFix round ${state.round}. Address the blocking findings and failed verification below.\n${JSON.stringify(state.review)}\n${JSON.stringify(state.verification)}` : '';
      const report = await answer(state.builder!, builderPrompt(input) + repair, `${input.id}:build:${state.round}`, runtime, context);
      await advance(input, { ...state, report, phase: 'artifact' }, runtime, context);
    },
    artifact: async (input, state, runtime, context) => {
      const gap = await optionalFile(join(input.worktree, '.romp/blocked.md'));
      if (gap.trim()) { await advance(input, { ...state, phase: 'finish', outcome: 'blocked', detail: gap.trim() }, runtime, context); return; }
      const artifact = parseArtifact(await optionalFile(join(input.worktree, '.romp/pull-request.md')));
      await advance(input, { ...state, artifact, phase: 'commit' }, runtime, context);
    },
    commit: async (input, state, runtime, context) => {
      await rm(join(input.worktree, '.romp/pull-request.md'), { force: true });
      const sha = await git.commit(input, state.artifact!.commit, runtime.signal);
      if (!await git.diff(input, runtime.signal)) { await advance(input, { ...state, phase: 'finish', outcome: 'no-changes' }, runtime, context); return; }
      await advance(input, { ...state, sha, phase: 'verify' }, runtime, context);
    },
    verify: async (input, state, runtime, context) => {
      if (await git.head(input, runtime.signal) !== state.sha) throw new Error('HEAD changed before verification');
      const verification = await git.verify(input, runtime.signal);
      await git.clean(input, runtime.signal);
      if (await git.head(input, runtime.signal) !== state.sha) throw new Error('HEAD changed during verification');
      const failed = verification.some((v) => v.exitCode !== 0);
      if (failed && state.prUrl && state.round < input.config.maxFixRounds) {
        await advance(input, { ...state, verification, round: state.round + 1, phase: 'build' }, runtime, context); return;
      }
      await advance(input, { ...state, verification, phase: failed ? 'finish' : 'publish', outcome: failed ? 'red' : '',
        detail: failed ? verification.find((v) => v.exitCode !== 0)!.output : '' }, runtime, context);
    },
    publish: async (input, state, runtime, context) => {
      await git.push(input, state.sha, runtime.signal);
      const marker = `<!-- romp-v2:${input.id} -->`;
      const existing = await gh.pullRequest(input.repo, input.branch, runtime.signal);
      if (existing && (existing.baseRefName !== input.base || !existing.body.includes(marker) || existing.state !== 'OPEN')) throw new Error('Existing PR does not match the durable job');
      const prUrl = existing?.url ?? await gh.createPullRequest(input.repo, input.branch, input.base, state.artifact!.title,
        `${state.artifact!.body}\n\nCloses #${input.issue.number}\n\n${marker}`, runtime.signal);
      await advance(input, { ...state, prUrl, phase: input.config.review ? 'review' : 'finish', outcome: input.config.review ? '' : 'done' }, runtime, context);
    },
    review: startAgent(true),
    reviewing: async (input, state, runtime, context) => {
      await git.clean(input, runtime.signal);
      if (await git.head(input, runtime.signal) !== state.sha) throw new Error('HEAD changed before review');
      const diff = await git.diff(input, runtime.signal);
      const prompt = `Independently review this change against the issue and repository conventions. Inspect correctness, specification compliance, test coverage, maintainability, and security.
Your environment permits reads only. Read relevant source files with the read tool.
Issue: ${input.issue.title}\n${input.issue.body}
Base: ${input.baseSha}\nHead: ${state.sha}
Verification: ${JSON.stringify(state.verification)}
Builder report: ${state.report}
Previous findings: ${JSON.stringify(state.review)}
Diff:\n${diff}
Return only strict JSON: {"verdict":"approve"|"fix","findings":[{"severity":"blocking"|"nit","file":"path","line":1,"description":"specific defect"}]}.
Use fix exactly when blocking findings exist. Do not stop after the first defect.`;
      const review = parseReview(await answer(state.reviewer!, prompt, `${input.id}:review:${state.round}`, runtime, context));
      await git.clean(input, runtime.signal);
      if (await git.head(input, runtime.signal) !== state.sha) throw new Error('HEAD changed during review');
      await advance(input, { ...state, review, phase: 'decide' }, runtime, context);
    },
    decide: async (input, state, runtime, context) => {
      await gh.comment(input.repo, state.prUrl, reviewComment(state.round, state.review!), `<!-- romp-v2:${input.id}:review:${state.round} -->`, runtime.signal);
      const fix = state.review!.verdict === 'fix';
      if (fix && state.round < input.config.maxFixRounds) {
        await advance(input, { ...state, round: state.round + 1, phase: 'build' }, runtime, context); return;
      }
      await advance(input, { ...state, phase: 'finish', outcome: fix ? 'changes-requested' : 'done' }, runtime, context);
    },
    finish: async (input, state, runtime, context) => {
      if (!state.outcome) throw new Error('Terminal outcome is missing');
      if (state.detail) await gh.comment(input.repo, state.prUrl || input.issue.number, `Romp v2 ${state.outcome}:\n\n${state.detail}`, `<!-- romp-v2:${input.id}:terminal -->`, runtime.signal);
      await gh.release(input.repo, input.issue.number, input.config, state.outcome, runtime.signal);
      if (['done', 'blocked', 'no-changes'].includes(state.outcome)) await git.cleanup(input, runtime.signal);
      await runtime.commit(async (tx) => {
        const job = (await tx.doc(State)).jobs[input.id]!;
        job.outcome = state.outcome;
        job.phase = 'finished';
        job.finishedAt = runtime.now();
        job.detail = state.detail;
        return { status: 'terminal', outcome: { status: 'completed', result: state.outcome as JobOutcome } };
      }, context);
    },
  };
  const phases = Object.fromEntries(Object.entries(handlers).map(([phase, handler]) => [phase,
    async (task: { input: JobInput; state: { checkpoint: Checkpoint } }, runtime: Runtime, context: Context) => {
      try { await handler(task.input, task.state.checkpoint, runtime, context); }
      catch (error) {
        if (runtime.signal.aborted) throw error;
        if (phase === 'finish') throw error;
        await advance(task.input, { ...task.state.checkpoint, phase: 'finish', outcome: 'failed', detail: message(error) }, runtime, context);
      }
    },
  ])) as Record<Phase, (task: { input: JobInput; state: { checkpoint: Checkpoint } }, runtime: Runtime, context: Context) => Promise<void>>;
  return defineTask<JobInput, Checkpoint, JobOutcome>({
    name: 'romp.delivery', version: 1,
    initial: () => ({ phase: 'prepare', round: 0, builder: null, reviewer: null, report: '', artifact: null,
      verification: [], sha: '', prUrl: '', review: null, outcome: '', detail: '' }),
    phases,
    abort: async (task, runtime, context) => {
      const reason = (await runtime.snapshot(State, context))!.jobs[task.input.id]!.cancellation || 'cancelled';
      let detail = `Job ${reason}; worktree preserved`;
      try { await gh.release(task.input.repo, task.input.issue.number, task.input.config, reason, runtime.signal); }
      catch (error) { detail += `; claim release failed: ${message(error)}`; }
      await runtime.commit(async (tx) => {
        const job = (await tx.doc(State)).jobs[task.input.id]!;
        job.phase = 'finished'; job.outcome = reason; job.detail = detail; job.finishedAt = runtime.now();
        return { status: 'terminal', outcome: { status: 'aborted', result: reason } };
      }, context);
    },
  });
}
