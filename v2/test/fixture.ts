import { mkdtemp, readFile, writeFile, mkdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { stringify } from 'smol-toml';
import { createModels } from '@earendil-works/pi-ai/models';
import { fauxProvider, fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai/providers/faux';
import type { Config, Issue } from '../src/domain.js';
import type { GitHub, PullRequest } from '../src/github.js';
import { command } from '../src/process.js';

export const issue: Issue = { number: 17, title: 'Write the answer', body: 'Change answer.txt to 42. Acceptance: answer.txt must contain exactly 42. Do not change other source files.', url: 'https://github.com/test/repo/issues/17', labels: [] };
export class TestGitHub implements GitHub {
  afterCreate?: () => Promise<void>;
  constructor(private readonly directory: string) {}
  async read(): Promise<{ creates: number; releases: string[]; comments: string[]; pr?: PullRequest }> {
    try { return JSON.parse(await readFile(join(this.directory, 'github.json'), 'utf8')); }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; return { creates: 0, releases: [], comments: [] }; }
  }
  async write(value: Awaited<ReturnType<TestGitHub['read']>>) { await writeFile(join(this.directory, 'github.json'), JSON.stringify(value)); }
  async identity(_root: string) { return { repo: 'test/repo', base: 'main' }; }
  async preflight(_root: string) {}
  async issue(_repo: string, _number: number) { return issue; }
  async issues(_repo: string, _label: string) { return [issue]; }
  async claim(_repo: string, _number: number, _config: Config, _signal: AbortSignal) {}
  async release(_repo: string, _number: number, _config: Config, outcome: string, _signal?: AbortSignal) {
    const data = await this.read(); data.releases.push(outcome); await this.write(data);
  }
  async pullRequest(_repo: string, _branch: string, _signal: AbortSignal) { return (await this.read()).pr; }
  async createPullRequest(_repo: string, _branch: string, base: string, _title: string, body: string, _signal: AbortSignal) {
    const data = await this.read(); data.creates++; data.pr = { url: 'https://github.com/test/repo/pull/18', body, baseRefName: base, state: 'OPEN' };
    await this.write(data); await this.afterCreate?.(); return data.pr.url;
  }
  async comment(_repo: string, _target: string | number, body: string, marker: string, _signal?: AbortSignal) {
    const data = await this.read(); if (!data.comments.some((c) => c.includes(marker))) { data.comments.push(body + marker); await this.write(data); }
  }
}
export function scriptedModels() {
  const faux = fauxProvider({ models: [{ id: 'test-model' }], tokensPerSecond: Infinity });
  const models = createModels(); models.setProvider(faux.provider);
  return { models, faux };
}
export const artifact = '---\ntitle: Write the answer\ncommit: fix: write the answer\n---\nSet answer.txt to 42. Verified with the configured command.';
export const buildResponses = (answer = '42') => [
  fauxAssistantMessage([fauxToolCall('write', { path: 'answer.txt', content: answer }),
    fauxToolCall('write', { path: '.romp/pull-request.md', content: artifact })], { stopReason: 'toolUse' }),
  fauxAssistantMessage('Changed answer.txt and ran verification.'),
];
export const approve = () => fauxAssistantMessage('{"verdict":"approve","findings":[]}');
export async function fixture(options: { verify?: string; review?: boolean; maxFixRounds?: number } = {}) {
  const directory = await mkdtemp(join(tmpdir(), 'romp-v2-test-'));
  const root = join(directory, 'repo'); const remote = join(directory, 'remote.git'); const state = join(directory, 'state');
  await mkdir(root);
  await command('git', ['init', '--bare', remote], directory);
  await command('git', ['init', '-b', 'main'], root);
  await command('git', ['config', 'user.name', 'Romp Test'], root);
  await command('git', ['config', 'user.email', 'test@example.invalid'], root);
  await command('git', ['config', 'commit.gpgsign', 'false'], root);
  await writeFile(join(root, 'answer.txt'), '0');
  await writeFile(join(root, 'romp-v2.toml'), stringify({
    label: 'romp:v2', verify: { commands: [options.verify ?? `node -e "if(require('node:fs').readFileSync('answer.txt','utf8')!=='42')process.exit(1)"`] },
    agent: { provider: 'faux', model: 'test-model' }, review: { enabled: options.review ?? true, max_fix_rounds: options.maxFixRounds ?? 2 },
  }));
  await command('git', ['add', '.'], root); await command('git', ['commit', '-m', 'chore: initialize test repository'], root);
  await command('git', ['remote', 'add', 'origin', remote], root); await command('git', ['push', '-u', 'origin', 'main'], root);
  return { directory, root, remote, state, gh: new TestGitHub(directory), cleanup: () => rm(directory, { recursive: true, force: true }) };
}
