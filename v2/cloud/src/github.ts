import { z } from 'zod';
import type { GitHub, PullRequest } from '../../src/application/ports.js';
import type { Config, JobOutcome } from '../../src/domain/model.js';
import type { Bindings, Submission } from './config.js';
const prSchema = z.object({
  html_url: z.string().url(),
  body: z.string().nullable(),
  base: z.object({ ref: z.string() }),
  head: z.object({ ref: z.string() }),
  state: z.enum(['open', 'closed']),
  merged_at: z.string().nullable(),
});
function base64(data: Uint8Array) {
  return btoa(String.fromCharCode(...data))
    .replace(/=/g, '')
    .replace(/\+/g, '-')
    .replace(/\//g, '_');
}
function der(tag: number, bytes: Uint8Array) {
  const n = bytes.length;
  const length = n < 128 ? [n] : n < 256 ? [129, n] : [130, n >> 8, n & 255];
  return Uint8Array.from([tag, ...length, ...bytes]);
}
export async function appJwt(id: string, pem: string, now = Date.now()) {
  let bytes = Uint8Array.from(atob(pem.replace(/-----[^-]+-----|\s/g, '')), (c) => c.charCodeAt(0));
  if (pem.includes('BEGIN RSA PRIVATE KEY'))
    bytes = der(
      48,
      Uint8Array.from([
        2,
        1,
        0,
        48,
        13,
        6,
        9,
        42,
        134,
        72,
        134,
        247,
        13,
        1,
        1,
        1,
        5,
        0,
        ...der(4, bytes),
      ]),
    );
  const key = await crypto.subtle.importKey(
    'pkcs8',
    bytes,
    { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' },
    false,
    ['sign'],
  );
  const seconds = Math.floor(now / 1000);
  const input = `${base64(new TextEncoder().encode(JSON.stringify({ alg: 'RS256', typ: 'JWT' })))}.${base64(new TextEncoder().encode(JSON.stringify({ iat: seconds - 60, exp: seconds + 540, iss: id })))}`;
  return `${input}.${base64(new Uint8Array(await crypto.subtle.sign('RSASSA-PKCS1-v1_5', key, new TextEncoder().encode(input))))}`;
}
export class GitHubApp implements GitHub {
  private credential: { token: string; expires: number } | undefined;
  constructor(
    private readonly env: Pick<Bindings, 'GITHUB_APP_ID' | 'GITHUB_PRIVATE_KEY'>,
    private readonly submission: Submission,
  ) {}
  async token() {
    if (this.credential && this.credential.expires > Date.now() + 60_000)
      return this.credential.token;
    const response = await fetch(
      `https://api.github.com/app/installations/${this.submission.repository.installationId}/access_tokens`,
      {
        method: 'POST',
        headers: this.headers(await appJwt(this.env.GITHUB_APP_ID, this.env.GITHUB_PRIVATE_KEY)),
        body: JSON.stringify({
          repository_ids: [this.submission.repositoryId],
          permissions: { contents: 'write', issues: 'write', pull_requests: 'write' },
        }),
        signal: AbortSignal.timeout(30_000),
      },
    );
    if (!response.ok) throw new Error(`GitHub token request failed: ${response.status}`);
    const data = z
      .object({ token: z.string().min(1), expires_at: z.string().datetime() })
      .parse(await response.json());
    this.credential = { token: data.token, expires: Date.parse(data.expires_at) };
    return data.token;
  }
  private headers(token: string) {
    return {
      authorization: `Bearer ${token}`,
      accept: 'application/vnd.github+json',
      'content-type': 'application/json',
      'user-agent': 'romp-cloud',
      'x-github-api-version': '2022-11-28',
    };
  }
  private scope(repo: string) {
    if (repo !== this.submission.repository.repo)
      throw new Error('Repository outside installation scope');
  }
  async request(
    path: string,
    method = 'GET',
    body?: unknown,
    signal?: AbortSignal,
    missing = false,
  ): Promise<unknown> {
    const response = await fetch(`https://api.github.com${path}`, {
      method,
      headers: this.headers(await this.token()),
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      signal: signal
        ? AbortSignal.any([signal, AbortSignal.timeout(30_000)])
        : AbortSignal.timeout(30_000),
    });
    if (missing && response.status === 404) {
      await response.body?.cancel();
      return null;
    }
    if (!response.ok) throw new Error(`GitHub ${method} failed: ${response.status}`);
    if (response.status === 204) return null;
    return response.json();
  }
  async preflight(_root: string) {
    await this.token();
  }
  async identity(_root: string) {
    const data = z
      .object({ default_branch: z.string().min(1) })
      .parse(await this.request(`/repos/${this.submission.repository.repo}`));
    return { repo: this.submission.repository.repo, base: data.default_branch };
  }
  async issue(repo: string, number: number) {
    this.scope(repo);
    const data = z
      .object({
        number: z.number(),
        title: z.string(),
        body: z.string().nullable(),
        html_url: z.string(),
        state: z.enum(['open', 'closed']),
        labels: z.array(z.object({ name: z.string() })),
        pull_request: z.unknown().optional(),
      })
      .parse(await this.request(`/repos/${repo}/issues/${number}`));
    if (data.state !== 'open' || data.pull_request) throw new Error('Issue is not open');
    return {
      number: data.number,
      title: data.title,
      body: data.body ?? '',
      url: data.html_url,
      labels: data.labels.map((v) => v.name),
    };
  }
  async issues(_repo: string, _label: string) {
    return [await this.issue(this.submission.repository.repo, this.submission.issue.number)];
  }
  async claim(repo: string, number: number, config: Config, signal: AbortSignal) {
    this.scope(repo);
    await this.request(
      `/repos/${repo}/issues/${number}/labels`,
      'POST',
      { labels: [config.claimedLabel] },
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
    this.scope(repo);
    await this.request(
      `/repos/${repo}/issues/${number}/labels/${encodeURIComponent(config.claimedLabel)}`,
      'DELETE',
      undefined,
      signal,
      true,
    );
    if (outcome !== 'blocked')
      await this.request(
        `/repos/${repo}/issues/${number}/labels/${encodeURIComponent(config.label)}`,
        'DELETE',
        undefined,
        signal,
        true,
      );
    const labels =
      outcome === 'blocked'
        ? [config.blockedLabel]
        : outcome === 'changes-requested'
          ? [config.changesRequestedLabel]
          : [];
    if (labels.length)
      await this.request(`/repos/${repo}/issues/${number}/labels`, 'POST', { labels }, signal);
  }
  async pullRequest(
    repo: string,
    branch: string,
    signal: AbortSignal,
  ): Promise<PullRequest | undefined> {
    this.scope(repo);
    const owner = repo.split('/')[0];
    const prs = z
      .array(prSchema)
      .parse(
        await this.request(
          `/repos/${repo}/pulls?state=all&head=${encodeURIComponent(`${owner}:${branch}`)}&per_page=100`,
          'GET',
          undefined,
          signal,
        ),
      )
      .filter((p) => p.head.ref === branch);
    if (prs.length > 1) throw new Error('Multiple PRs for run branch');
    const pr = prs[0];
    return pr
      ? {
          url: pr.html_url,
          body: pr.body ?? '',
          baseRefName: pr.base.ref,
          state: pr.merged_at ? 'MERGED' : pr.state === 'open' ? 'OPEN' : 'CLOSED',
        }
      : undefined;
  }
  async createPullRequest(
    repo: string,
    branch: string,
    base: string,
    title: string,
    body: string,
    signal: AbortSignal,
  ) {
    this.scope(repo);
    return prSchema.parse(
      await this.request(
        `/repos/${repo}/pulls`,
        'POST',
        { head: branch, base, title, body },
        signal,
      ),
    ).html_url;
  }
  async comment(
    repo: string,
    target: string | number,
    body: string,
    marker: string,
    signal?: AbortSignal,
  ) {
    this.scope(repo);
    const number = typeof target === 'string' ? target.split('/').at(-1) : target;
    if (!/^\d+$/.test(String(number))) throw new Error('Invalid comment target');
    for (let page = 1; ; page++) {
      const comments = z
        .array(z.object({ body: z.string() }))
        .parse(
          await this.request(
            `/repos/${repo}/issues/${number}/comments?per_page=100&page=${page}`,
            'GET',
            undefined,
            signal,
          ),
        );
      if (comments.some((c) => c.body.includes(marker))) return;
      if (comments.length < 100) break;
    }
    await this.request(
      `/repos/${repo}/issues/${number}/comments`,
      'POST',
      { body: `${body}\n\n${marker}` },
      signal,
    );
  }
}
