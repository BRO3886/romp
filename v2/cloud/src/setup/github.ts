import { z } from 'zod';
import { appJwt } from '../github.js';
import type { Profile, SetupDraft } from './profile.js';
import { jsonRequest, type Fetch } from './http.js';
const permissions = z.object({
  contents: z.literal('write'),
  issues: z.literal('write'),
  pull_requests: z.literal('write'),
});
const hookSchema = z.object({
  url: z.string(),
  content_type: z.string(),
  insecure_ssl: z.union([z.string(), z.number()]),
});
async function githubHeaders(profile: Pick<Profile, 'appId' | 'secrets'>) {
  const jwt = await appJwt(profile.appId, profile.secrets.GITHUB_PRIVATE_KEY);
  return {
    authorization: `Bearer ${jwt}`,
    accept: 'application/vnd.github+json',
    'x-github-api-version': '2022-11-28',
    'user-agent': 'romp-cloud-setup',
    'content-type': 'application/json',
  };
}
async function repositoryHeaders(
  profile: Pick<Profile, 'appId' | 'secrets'>,
  repo: string,
  installationId: number,
  fetcher: Fetch,
) {
  const headers = await githubHeaders(profile);
  const token = z.object({ token: z.string().min(1) }).parse(
    await jsonRequest(
      fetcher,
      `https://api.github.com/app/installations/${installationId}/access_tokens`,
      {
        method: 'POST',
        headers,
        body: JSON.stringify({
          repositories: [repo.split('/')[1]],
          permissions: { contents: 'write', issues: 'write', pull_requests: 'write' },
        }),
      },
    ),
  );
  return { ...headers, authorization: `Bearer ${token.token}` };
}
export async function githubPreflight(profile: SetupDraft, fetcher: Fetch, replaceHook: boolean) {
  const headers = await githubHeaders(profile);
  const app = z
    .object({ id: z.number(), events: z.array(z.string()), permissions })
    .parse(await jsonRequest(fetcher, 'https://api.github.com/app', { headers }));
  if (String(app.id) !== profile.appId || !app.events.includes('issues'))
    throw new Error('GitHub App must subscribe to issues events');
  const hook = hookSchema.parse(
    await jsonRequest(fetcher, 'https://api.github.com/app/hook/config', { headers }),
  );
  if (hook.url && hook.url !== `${profile.endpoint}/webhook` && !replaceHook)
    throw new Error(
      `App webhook already points to ${hook.url}. Use a dedicated App or --replace-webhook after reviewing this change`,
    );
  const repositories: Profile['repositories'] = [];
  for (const repository of profile.repositories) {
    const installation = z
      .object({ id: z.number().int().positive(), permissions, suspended_at: z.string().nullable() })
      .parse(
        await jsonRequest(fetcher, `https://api.github.com/repos/${repository.repo}/installation`, {
          headers,
        }),
      );
    if (installation.suspended_at)
      throw new Error(`GitHub installation is suspended for ${repository.repo}`);
    const repositoryAuth = await repositoryHeaders(
      profile,
      repository.repo,
      installation.id,
      fetcher,
    );
    const repo = z.object({ full_name: z.string(), has_issues: z.boolean() }).parse(
      await jsonRequest(fetcher, `https://api.github.com/repos/${repository.repo}`, {
        headers: repositoryAuth,
      }),
    );
    if (repo.full_name.toLowerCase() !== repository.repo.toLowerCase() || !repo.has_issues)
      throw new Error(`Issues must be enabled for ${repository.repo}`);
    repositories.push({ ...repository, repo: repo.full_name, installationId: installation.id });
  }
  return { repositories, hook };
}
export async function ensureLabels(profile: Profile, fetcher: Fetch) {
  for (const repository of profile.repositories) {
    const headers = await repositoryHeaders(
      profile,
      repository.repo,
      repository.installationId,
      fetcher,
    );
    const { label, claimedLabel, blockedLabel, changesRequestedLabel } = repository.config;
    for (const name of new Set([label, claimedLabel, blockedLabel, changesRequestedLabel])) {
      const url = `https://api.github.com/repos/${repository.repo}/labels/${encodeURIComponent(name)}`;
      const read = () =>
        fetcher(url, { headers, redirect: 'error', signal: AbortSignal.timeout(30_000) });
      const response = await read();
      if (response.ok) continue;
      if (response.status !== 404)
        throw new Error(`Cannot read repository labels (HTTP ${response.status})`);
      const created = await fetcher(`https://api.github.com/repos/${repository.repo}/labels`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ name, color: 'C0C0C0' }),
        redirect: 'error',
        signal: AbortSignal.timeout(30_000),
      });
      if (created.ok) continue;
      if (created.status === 422 && (await read()).ok) continue;
      throw new Error(`Cannot create repository label (HTTP ${created.status})`);
    }
  }
}
export async function connectWebhook(profile: Profile, fetcher: Fetch) {
  const desired = {
    url: `${profile.endpoint}/webhook`,
    content_type: 'json',
    insecure_ssl: '0',
    secret: profile.secrets.GITHUB_WEBHOOK_SECRET,
  };
  hookSchema.parse(
    await jsonRequest(fetcher, 'https://api.github.com/app/hook/config', {
      method: 'PATCH',
      headers: await githubHeaders(profile),
      body: JSON.stringify(desired),
    }),
  );
  await verifyWebhook(profile, fetcher);
}
export async function verifyWebhook(profile: Profile, fetcher: Fetch) {
  const hook = hookSchema.parse(
    await jsonRequest(fetcher, 'https://api.github.com/app/hook/config', {
      headers: await githubHeaders(profile),
    }),
  );
  if (
    hook.url !== `${profile.endpoint}/webhook` ||
    hook.content_type !== 'json' ||
    String(hook.insecure_ssl) !== '0'
  )
    throw new Error('GitHub webhook configuration does not match this deployment');
}
