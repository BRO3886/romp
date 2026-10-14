import { createModels } from '@earendil-works/pi-ai/models';
import {
  fauxProvider,
  fauxAssistantMessage,
  fauxToolCall,
} from '@earendil-works/pi-ai/providers/faux';
import { z } from 'zod';
import { IssueRun as ProductionRun } from '../src/run.js';
import type { Sandbox } from '../src/sandbox.js';
import type { Submission } from '../src/config.js';
import type { GitHub, PullRequest } from '../../src/application/ports.js';
import type { Config, JobOutcome } from '../../src/domain/model.js';
export { Scheduler } from '../src/scheduler.js';
export { default } from '../src/worker.js';
type GitState = { creates: number; pr?: PullRequest; releases: number; comments: string[] };
export class IssueRun extends ProductionRun {
  private submission: Submission | undefined;
  private faultAfterPR = false;
  fault(value: boolean) {
    this.faultAfterPR = value;
  }
  async evidence() {
    return this.ctx.storage.get<GitState>('github');
  }
  async destroyWorkspace() {
    await this.createSandbox().destroy();
  }
  async interrupt() {
    if (this.runtime) await (await this.runtime).close();
  }
  override async start(value: Submission) {
    this.submission = value;
    return super.start(value);
  }
  protected override createSandbox(): Sandbox {
    const id = this.submission?.id;
    if (!id) throw new Error('Missing test identity');
    const url = z.string().url().parse(Reflect.get(this.env, 'TEST_RUNNER_URL'));
    const request = async (action: string, body?: unknown) => {
      const response = await fetch(`${url}/${id}/${action}`, {
        method: 'POST',
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      if (!response.ok) throw new Error(await response.text());
      return response.json();
    };
    return {
      exec: async (args, options) =>
        z
          .object({ stdout: z.string(), stderr: z.string(), exitCode: z.number() })
          .parse(await request('exec', { args, cwd: options?.cwd })),
      checkpoint: async () => {
        await request('checkpoint');
      },
      destroy: async () => {
        if (await this.ctx.storage.get('fail-destroy')) {
          await this.ctx.storage.delete('fail-destroy');
          throw new Error('Injected destroy failure');
        }
        await request('destroy');
      },
    };
  }
  protected override createModels() {
    const model = fauxProvider({
      provider: 'openai',
      models: [{ id: 'gpt-6.1-sol' }],
      tokensPerSecond: Infinity,
    });
    const responses = [
      fauxAssistantMessage(
        [
          fauxToolCall('write', { path: 'answer.txt', content: '42' }),
          fauxToolCall('write', {
            path: '.romp/pull-request.md',
            content:
              '---\ntitle: Change answer\ncommit: fix: change answer\n---\nSet answer.txt to 42.',
          }),
        ],
        { stopReason: 'toolUse' },
      ),
      fauxAssistantMessage('Updated answer.txt'),
    ];
    model.setResponses(
      responses.map((response) => async () => {
        await this.ctx.storage.put(
          'model-calls',
          ((await this.ctx.storage.get<number>('model-calls')) ?? 0) + 1,
        );
        return response;
      }),
    );
    const models = createModels();
    models.setProvider(model.provider);
    return models;
  }
  protected override createGitHub(submission: Submission): GitHub & { token(): Promise<string> } {
    const read = async (): Promise<GitState> =>
      (await this.ctx.storage.get<GitState>('github')) ?? { creates: 0, releases: 0, comments: [] };
    return {
      token: () => Promise.resolve('test-token'),
      preflight: () => Promise.resolve(),
      identity: () => Promise.resolve({ repo: submission.repository.repo, base: 'main' }),
      issue: () => Promise.resolve(submission.issue),
      issues: () => Promise.resolve([submission.issue]),
      claim: () => Promise.resolve(),
      release: async (_repo: string, _number: number, _config: Config, _outcome: JobOutcome) => {
        const state = await read();
        state.releases++;
        await this.ctx.storage.put('github', state);
      },
      pullRequest: async () => (await read()).pr,
      createPullRequest: async (_repo, _branch, base, _title, body, signal) => {
        const state = await read();
        state.creates++;
        state.pr = {
          url: `https://github.com/test/repo/pull/${submission.issue.number + 100}`,
          body,
          baseRefName: base,
          state: 'OPEN',
        };
        await this.ctx.storage.put('github', state);
        if (this.faultAfterPR)
          await new Promise<void>((resolve) =>
            signal.addEventListener('abort', () => resolve(), { once: true }),
          );
        return state.pr.url;
      },
      comment: async (_repo, _target, body, marker) => {
        const state = await read();
        if (!state.comments.some((v) => v.includes(marker))) state.comments.push(body + marker);
        await this.ctx.storage.put('github', state);
      },
    };
  }
}
