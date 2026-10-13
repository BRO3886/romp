import type { Context } from '@earendil-works/chord';
import {
  AssistantEntry,
  configure,
  defineTask,
  type Extension,
  type RunningTask,
  type TaskRuntime,
} from '@earendil-works/pi-durable';
import type { GitHub, Workspace } from '../../application/ports.js';
import {
  builderPrompt,
  parseArtifact,
  parseReview,
  reviewComment,
} from '../../domain/contracts.js';
import {
  CheckpointSchema,
  initialCheckpoint,
  type Checkpoint,
  type PhaseCheckpoint,
} from '../../domain/checkpoint.js';
import { JobInputSchema, type JobInput, type JobOutcome, type Phase } from '../../domain/model.js';
import { message, requireValue, requireEntry } from '../../shared/errors.js';
import { State, Role } from './documents.js';
import { conversationId } from './ids.js';

type Runtime = TaskRuntime<JobInput, Checkpoint, JobOutcome, object>;
type Handler<P extends Phase> = (
  input: JobInput,
  state: PhaseCheckpoint<P>,
  runtime: Runtime,
  context: Context,
) => Promise<void>;
type Handlers = { [P in Phase]: Handler<P> };
async function advance(input: JobInput, state: Checkpoint, runtime: Runtime, context: Context) {
  await runtime.commit(async (tx) => {
    const job = requireEntry((await tx.doc(State)).jobs, input.id, 'Durable job record is missing');
    job.phase = state.phase;
    job.prUrl = state.prUrl;
    job.detail = state.detail;
    if (['artifact', 'publish', 'decide', 'finish'].includes(state.phase)) {
      job.events.push({
        at: runtime.now(),
        phase: state.phase,
        round: state.round,
        sha: state.sha,
        report: state.phase === 'artifact' ? state.report : '',
        verification:
          state.phase === 'publish' || state.phase === 'finish' ? state.verification : [],
        review: state.phase === 'decide' ? state.review : null,
      });
    }
    return { status: 'running', checkpoint: state };
  }, context);
}
async function answer(
  id: number,
  prompt: string,
  requestId: string,
  runtime: Runtime,
  context: Context,
): Promise<string> {
  const conversation = requireValue(
    await runtime.conversation(conversationId(id), context),
    'Agent conversation is missing',
  );
  const settled = await (
    await conversation.submit({ type: 'input', content: prompt, requestId }, context)
  ).wait(context);
  if (settled.status !== 'done' || settled.type !== 'input')
    throw new Error(`Agent input ended ${settled.status}`);
  let output = '';
  await runtime.commit(async (tx) => {
    const entry = requireValue(
      await tx.entry(AssistantEntry, settled.answer),
      'Agent answer is missing',
    );
    output =
      entry.model
        ?.flatMap((part) =>
          part.role === 'assistant'
            ? part.content.flatMap((item) => (item.type === 'text' ? [item.text] : []))
            : [],
        )
        .join('\n') ?? '';
    return undefined;
  }, context);
  if (!output.trim()) throw new Error('Agent completed without a final text report');
  return output;
}
function guarded<P extends Phase>(handler: Handler<P>) {
  return async (
    task: RunningTask<JobInput, PhaseCheckpoint<P>, JobOutcome>,
    runtime: Runtime,
    context: Context,
  ) => {
    try {
      await handler(task.input, task.state.checkpoint, runtime, context);
    } catch (error) {
      if (runtime.signal.aborted || task.state.checkpoint.phase === 'finish') throw error;
      await advance(
        task.input,
        { ...task.state.checkpoint, phase: 'finish', outcome: 'failed', detail: message(error) },
        runtime,
        context,
      );
    }
  };
}
export function deliveryTask(
  github: GitHub,
  workspace: Workspace,
  extensions: { builder: Extension; reviewer: Extension },
) {
  async function createAgent(
    input: JobInput,
    readOnly: boolean,
    next: (id: number) => Checkpoint,
    runtime: Runtime,
    context: Context,
  ) {
    await runtime.commit(async (tx) => {
      const job = requireEntry(
        (await tx.doc(State)).jobs,
        input.id,
        'Durable job record is missing',
      );
      const conversation = await tx.createConversation({
        ownership: { kind: 'task', taskId: runtime.taskId },
      });
      await configure(tx, conversation.id, {
        model: readOnly ? input.config.reviewModel : input.config.model,
        cwd: input.worktree,
        extensions: [readOnly ? extensions.reviewer : extensions.builder],
      });
      (await tx.doc(Role, conversation.id)).readOnly = readOnly;
      const state = next(conversation.id);
      job.phase = state.phase;
      return { status: 'running', checkpoint: state };
    }, context);
  }
  const handlers = {
    prepare: async (input, state, runtime, context) => {
      await github.claim(input.repo, input.issue.number, input.config, runtime.signal);
      await workspace.prepare(input, runtime.signal);
      await advance(input, { ...state, phase: 'build' }, runtime, context);
    },
    build: async (input, state, runtime, context) => {
      const next = (builder: number): PhaseCheckpoint<'building'> => ({
        ...state,
        builder,
        phase: 'building',
      });
      if (state.builder !== null) await advance(input, next(state.builder), runtime, context);
      else await createAgent(input, false, next, runtime, context);
    },
    building: async (input, state, runtime, context) => {
      const repair = state.round
        ? `\n\nFix round ${state.round}. Address the blocking findings and failed verification below.\n${JSON.stringify(state.review)}\n${JSON.stringify(state.verification)}`
        : '';
      const report = await answer(
        state.builder,
        builderPrompt(input) + repair,
        `${input.id}:build:${state.round}`,
        runtime,
        context,
      );
      await advance(input, { ...state, report, phase: 'artifact' }, runtime, context);
    },
    artifact: async (input, state, runtime, context) => {
      const gap = await workspace.readArtifact(input, 'blocked.md');
      if (gap.trim()) {
        await advance(
          input,
          { ...state, phase: 'finish', outcome: 'blocked', detail: gap.trim() },
          runtime,
          context,
        );
        return;
      }
      const artifact = parseArtifact(await workspace.readArtifact(input, 'pull-request.md'));
      await advance(input, { ...state, artifact, phase: 'commit' }, runtime, context);
    },
    commit: async (input, state, runtime, context) => {
      await workspace.removeArtifact(input, 'pull-request.md');
      const sha = await workspace.commit(input, state.artifact.commit, runtime.signal);
      if (!(await workspace.diff(input, runtime.signal))) {
        await advance(
          input,
          { ...state, phase: 'finish', outcome: 'no-changes' },
          runtime,
          context,
        );
        return;
      }
      await advance(input, { ...state, sha, phase: 'verify' }, runtime, context);
    },
    verify: async (input, state, runtime, context) => {
      if ((await workspace.head(input, runtime.signal)) !== state.sha)
        throw new Error('HEAD changed before verification');
      const verification = await workspace.verify(input, runtime.signal);
      await workspace.clean(input, runtime.signal);
      if ((await workspace.head(input, runtime.signal)) !== state.sha)
        throw new Error('HEAD changed during verification');
      const failure = verification.find((result) => result.exitCode !== 0);
      if (failure && state.prUrl && state.round < input.config.maxFixRounds) {
        await advance(
          input,
          { ...state, verification, round: state.round + 1, phase: 'build' },
          runtime,
          context,
        );
        return;
      }
      if (failure)
        await advance(
          input,
          { ...state, verification, phase: 'finish', outcome: 'red', detail: failure.output },
          runtime,
          context,
        );
      else
        await advance(
          input,
          { ...state, verification, phase: 'publish', outcome: '', detail: '' },
          runtime,
          context,
        );
    },
    publish: async (input, state, runtime, context) => {
      await workspace.push(input, state.sha, runtime.signal);
      const marker = `<!-- romp-v2:${input.id} -->`;
      const existing = await github.pullRequest(input.repo, input.branch, runtime.signal);
      if (
        existing &&
        (existing.baseRefName !== input.base ||
          !existing.body.includes(marker) ||
          existing.state !== 'OPEN')
      )
        throw new Error('Existing PR does not match the durable job');
      const prUrl =
        existing?.url ??
        (await github.createPullRequest(
          input.repo,
          input.branch,
          input.base,
          state.artifact.title,
          `${state.artifact.body}\n\nCloses #${input.issue.number}\n\n${marker}`,
          runtime.signal,
        ));
      if (input.config.review)
        await advance(input, { ...state, prUrl, phase: 'review', outcome: '' }, runtime, context);
      else
        await advance(
          input,
          { ...state, prUrl, phase: 'finish', outcome: 'done' },
          runtime,
          context,
        );
    },
    review: async (input, state, runtime, context) => {
      const next = (reviewer: number): PhaseCheckpoint<'reviewing'> => ({
        ...state,
        reviewer,
        phase: 'reviewing',
      });
      if (state.reviewer !== null) await advance(input, next(state.reviewer), runtime, context);
      else await createAgent(input, true, next, runtime, context);
    },
    reviewing: async (input, state, runtime, context) => {
      await workspace.clean(input, runtime.signal);
      if ((await workspace.head(input, runtime.signal)) !== state.sha)
        throw new Error('HEAD changed before review');
      const diff = await workspace.diff(input, runtime.signal);
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
      const review = parseReview(
        await answer(state.reviewer, prompt, `${input.id}:review:${state.round}`, runtime, context),
      );
      await workspace.clean(input, runtime.signal);
      if ((await workspace.head(input, runtime.signal)) !== state.sha)
        throw new Error('HEAD changed during review');
      await advance(input, { ...state, review, phase: 'decide' }, runtime, context);
    },
    decide: async (input, state, runtime, context) => {
      await github.comment(
        input.repo,
        state.prUrl,
        reviewComment(state.round, state.review),
        `<!-- romp-v2:${input.id}:review:${state.round} -->`,
        runtime.signal,
      );
      if (state.review.verdict === 'fix' && state.round < input.config.maxFixRounds) {
        await advance(
          input,
          { ...state, round: state.round + 1, phase: 'build' },
          runtime,
          context,
        );
        return;
      }
      await advance(
        input,
        {
          ...state,
          phase: 'finish',
          outcome: state.review.verdict === 'fix' ? 'changes-requested' : 'done',
        },
        runtime,
        context,
      );
    },
    finish: async (input, state, runtime, context) => {
      if (state.detail)
        await github.comment(
          input.repo,
          state.prUrl || input.issue.number,
          `Romp v2 ${state.outcome}:\n\n${state.detail}`,
          `<!-- romp-v2:${input.id}:terminal -->`,
          runtime.signal,
        );
      await github.release(
        input.repo,
        input.issue.number,
        input.config,
        state.outcome,
        runtime.signal,
      );
      if (['done', 'blocked', 'no-changes'].includes(state.outcome))
        await workspace.cleanup(input, runtime.signal);
      await runtime.commit(async (tx) => {
        const job = requireEntry(
          (await tx.doc(State)).jobs,
          input.id,
          'Durable job record is missing',
        );
        job.outcome = state.outcome;
        job.phase = 'finished';
        job.finishedAt = runtime.now();
        job.detail = state.detail;
        return { status: 'terminal', outcome: { status: 'completed', result: state.outcome } };
      }, context);
    },
  } satisfies Handlers;
  return defineTask<JobInput, Checkpoint, JobOutcome>({
    name: 'romp.delivery',
    version: 2,
    initial: initialCheckpoint,
    migrate(input, checkpoint, fromVersion) {
      if (fromVersion !== 1)
        throw new Error(`Unsupported delivery checkpoint version ${fromVersion}`);
      return { input: JobInputSchema.parse(input), checkpoint: CheckpointSchema.parse(checkpoint) };
    },
    phases: {
      prepare: guarded(handlers.prepare),
      build: guarded(handlers.build),
      building: guarded(handlers.building),
      artifact: guarded(handlers.artifact),
      commit: guarded(handlers.commit),
      verify: guarded(handlers.verify),
      publish: guarded(handlers.publish),
      review: guarded(handlers.review),
      reviewing: guarded(handlers.reviewing),
      decide: guarded(handlers.decide),
      finish: guarded(handlers.finish),
    },
    abort: async (task, runtime, context) => {
      const state = requireValue(
        await runtime.snapshot(State, context),
        'Durable state is missing',
      );
      const reason =
        requireEntry(state.jobs, task.input.id, 'Durable job record is missing').cancellation ||
        'cancelled';
      let detail = `Job ${reason}; worktree preserved`;
      try {
        await github.release(
          task.input.repo,
          task.input.issue.number,
          task.input.config,
          reason,
          runtime.signal,
        );
      } catch (error) {
        detail += `; claim release failed: ${message(error)}`;
      }
      await runtime.commit(async (tx) => {
        const job = requireEntry(
          (await tx.doc(State)).jobs,
          task.input.id,
          'Durable job record is missing',
        );
        job.phase = 'finished';
        job.outcome = reason;
        job.detail = detail;
        job.finishedAt = runtime.now();
        return { status: 'terminal', outcome: { status: 'aborted', result: reason } };
      }, context);
    },
  });
}
