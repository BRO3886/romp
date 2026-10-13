import { z } from 'zod';
import { nonEmptyText, recordId } from '../../domain/model.js';
import { parseJson } from '../../shared/json.js';
import type { GitHub, PullRequest } from '../../application/ports.js';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { command } from './process.js';
import type { Config, Issue, JobOutcome } from '../../domain/model.js';

const WireIssueSchema = z
  .object({
    number: recordId,
    title: nonEmptyText,
    body: z.string().nullable(),
    url: nonEmptyText,
    html_url: nonEmptyText.optional(),
    labels: z.array(z.object({ name: nonEmptyText })),
    pull_request: z.unknown().optional(),
  })
  .passthrough();
const PullRequestSchema = z.object({
  url: nonEmptyText,
  body: z.string(),
  baseRefName: nonEmptyText,
  state: z.enum(['OPEN', 'CLOSED', 'MERGED']),
});
function issue(data: z.infer<typeof WireIssueSchema>): Issue {
  return {
    number: data.number,
    title: data.title,
    body: data.body ?? '',
    url: data.html_url ?? data.url,
    labels: data.labels.map((label) => label.name),
  };
}
export class GitHubCLI implements GitHub {
  constructor(private readonly cwd: string) {}
  private async run(args: string[], signal?: AbortSignal, cwd = this.cwd): Promise<string> {
    const bounded = signal
      ? AbortSignal.any([signal, AbortSignal.timeout(30_000)])
      : AbortSignal.timeout(30_000);
    return (
      await command('gh', args, cwd, bounded, {
        env: { ...process.env, GH_HOST: 'github.com', GH_REPO: undefined, GH_PROMPT_DISABLED: '1' },
      })
    ).stdout.trim();
  }
  async preflight(root: string) {
    await this.run(['auth', 'status'], undefined, root);
  }
  async identity(root: string) {
    const origin = (await command('git', ['remote', 'get-url', 'origin'], root)).stdout.trim();
    if (!/^(https:\/\/github\.com\/|git@github\.com:|ssh:\/\/git@github\.com\/)/.test(origin))
      throw new Error('V2 currently requires a GitHub.com origin remote');
    const data = z
      .object({ nameWithOwner: nonEmptyText, defaultBranchRef: z.object({ name: nonEmptyText }) })
      .parse(
        parseJson(
          await this.run(
            ['repo', 'view', '--json', 'nameWithOwner,defaultBranchRef'],
            undefined,
            root,
          ),
        ),
      );
    return { repo: data.nameWithOwner, base: data.defaultBranchRef.name };
  }
  async issue(repo: string, number: number) {
    return issue(
      WireIssueSchema.parse(
        parseJson(
          await this.run([
            'issue',
            'view',
            String(number),
            '--repo',
            repo,
            '--json',
            'number,title,body,url,labels',
          ]),
        ),
      ),
    );
  }
  async issues(repo: string, label: string) {
    const pages = z
      .array(z.array(WireIssueSchema))
      .parse(
        parseJson(
          await this.run([
            'api',
            '--paginate',
            '--slurp',
            `repos/${repo}/issues?state=open&labels=${encodeURIComponent(label)}&per_page=100`,
          ]),
        ),
      );
    return pages
      .flat()
      .filter((i) => !('pull_request' in i))
      .map(issue);
  }
  async claim(repo: string, number: number, config: Config, signal: AbortSignal) {
    await this.run(
      [
        'issue',
        'edit',
        String(number),
        '--repo',
        repo,
        '--add-label',
        config.claimedLabel,
        '--add-assignee',
        '@me',
      ],
      signal,
    );
  }
  async release(
    repo: string,
    number: number,
    config: Config,
    outcome: JobOutcome,
    signal?: AbortSignal,
  ) {
    const args = [
      'issue',
      'edit',
      String(number),
      '--repo',
      repo,
      '--remove-label',
      config.claimedLabel,
      '--remove-assignee',
      '@me',
    ];
    if (outcome === 'blocked') args.push('--add-label', config.blockedLabel);
    else args.push('--remove-label', config.label);
    if (outcome === 'changes-requested') args.push('--add-label', config.changesRequestedLabel);
    await this.run(args, signal);
  }
  async pullRequest(repo: string, branch: string, signal: AbortSignal) {
    const data: PullRequest[] = z
      .array(PullRequestSchema)
      .parse(
        parseJson(
          await this.run(
            [
              'pr',
              'list',
              '--repo',
              repo,
              '--head',
              branch,
              '--state',
              'all',
              '--json',
              'url,body,baseRefName,state',
            ],
            signal,
          ),
        ),
      );
    if (data.length > 1) throw new Error(`Multiple PRs exist for ${branch}`);
    return data[0];
  }
  async createPullRequest(
    repo: string,
    branch: string,
    base: string,
    title: string,
    body: string,
    signal: AbortSignal,
  ) {
    return this.withBody(body, (path) =>
      this.run(
        [
          'pr',
          'create',
          '--repo',
          repo,
          '--head',
          branch,
          '--base',
          base,
          '--title',
          title,
          '--body-file',
          path,
        ],
        signal,
      ),
    );
  }
  async comment(
    repo: string,
    target: string | number,
    body: string,
    marker: string,
    signal?: AbortSignal,
  ) {
    const number = typeof target === 'number' ? target : Number(target.split('/').at(-1));
    if (!Number.isSafeInteger(number) || number < 1) throw new Error('Invalid comment target');
    const pages = z
      .array(z.array(z.object({ body: z.string() })))
      .parse(
        parseJson(
          await this.run(
            [
              'api',
              '--paginate',
              '--slurp',
              `repos/${repo}/issues/${number}/comments?per_page=100`,
            ],
            signal,
          ),
        ),
      );
    if (pages.flat().some((c) => c.body.includes(marker))) return;
    await this.withBody(`${body}\n\n${marker}`, (path) =>
      this.run(['issue', 'comment', String(number), '--repo', repo, '--body-file', path], signal),
    );
  }
  private async withBody<T>(body: string, action: (path: string) => Promise<T>): Promise<T> {
    const directory = await mkdtemp(join(tmpdir(), 'romp-v2-body-'));
    const path = join(directory, 'body.md');
    try {
      await writeFile(path, body, { mode: 0o600 });
      return await action(path);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  }
}
