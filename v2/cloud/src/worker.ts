import { checkModels } from './models.js';
import { z } from 'zod';
import { appJwt } from './github.js';
import { preflight, runId, type Bindings } from './config.js';
import { authenticateAdmin, authenticateWebhook, boundedBody } from './http.js';
export { Scheduler } from './scheduler.js';
export { IssueRun } from './run.js';
const event = z.object({
  action: z.string(),
  installation: z.object({ id: z.number().int().positive() }),
  repository: z.object({ id: z.number().int().positive(), full_name: z.string() }),
  issue: z.object({
    number: z.number().int().positive(),
    title: z.string().min(1),
    body: z.string().nullable(),
    html_url: z.string().url(),
    labels: z.array(z.object({ name: z.string() })),
  }),
});
export default {
  async fetch(request: Request, env: Bindings): Promise<Response> {
    const path = new URL(request.url).pathname;
    if (path === '/health' && request.method === 'GET')
      return Response.json({ service: 'romp-cloud' });
    if (path === '/webhook' && request.method === 'POST') {
      let body: Uint8Array;
      try {
        body = await boundedBody(request);
      } catch {
        return new Response('Body too large', { status: 413 });
      }
      if (
        !(await authenticateWebhook(
          body,
          request.headers.get('x-hub-signature-256'),
          env.GITHUB_WEBHOOK_SECRET,
        ))
      )
        return new Response('Unauthorized', { status: 401 });
      const type = request.headers.get('x-github-event');
      if (type === 'ping') return new Response('pong');
      if (type !== 'issues') return new Response(null, { status: 204 });
      let payload: z.infer<typeof event>;
      try {
        payload = event.parse(JSON.parse(new TextDecoder().decode(body)) as unknown);
      } catch {
        return new Response('Invalid issue event', { status: 400 });
      }
      if (payload.action !== 'opened') return new Response(null, { status: 204 });
      const config = preflight(env);
      const repository = config.repositories.find(
        (r) =>
          r.repo.toLowerCase() === payload.repository.full_name.toLowerCase() &&
          r.installationId === payload.installation.id,
      );
      if (!repository) return new Response('Repository not enabled', { status: 403 });
      const id = runId(payload.repository.id, payload.issue.number);
      await env.SCHEDULER.getByName('installation').enqueue({
        id,
        repositoryId: payload.repository.id,
        repository,
        issue: {
          number: payload.issue.number,
          title: payload.issue.title,
          body: payload.issue.body ?? '',
          url: payload.issue.html_url,
          labels: payload.issue.labels.map((l) => l.name),
        },
      });
      return Response.json({ id }, { status: 202 });
    }
    if (!(await authenticateAdmin(request, env.ADMIN_TOKEN)))
      return new Response('Unauthorized', { status: 401 });
    const scheduler = env.SCHEDULER.getByName('installation');
    if (path === '/doctor' && request.method === 'GET') {
      try {
        const c = preflight(env);
        await appJwt(env.GITHUB_APP_ID, env.GITHUB_PRIVATE_KEY);
        await checkModels(c, env);
        return Response.json({
          ready: true,
          repositories: c.repositories.map((r) => r.repo),
          concurrency: c.concurrency,
        });
      } catch (error) {
        return Response.json(
          { ready: false, error: error instanceof Error ? error.message : 'Invalid configuration' },
          { status: 422 },
        );
      }
    }
    if (path === '/status' && request.method === 'GET')
      return Response.json(await scheduler.status());
    if ((path === '/pause' || path === '/resume') && request.method === 'POST')
      return Response.json(await scheduler.pause(path === '/pause'));
    if (path === '/concurrency' && request.method === 'PUT') {
      try {
        const body = z
          .object({ concurrency: z.number().int().min(1).max(32) })
          .strict()
          .parse(JSON.parse(new TextDecoder().decode(await boundedBody(request, 4096))) as unknown);
        return Response.json(await scheduler.configure(body.concurrency));
      } catch {
        return new Response('Invalid concurrency', { status: 400 });
      }
    }
    const match = /^\/runs\/(r\d+-i\d+)(\/cancel)?$/.exec(path);
    if (match?.[1]) {
      const run = env.RUNS.getByName(match[1]);
      if (request.method === 'GET' && !match[2]) return Response.json(await run.status());
      if (request.method === 'POST' && match[2]) {
        await scheduler.cancel(match[1]);
        return new Response(null, { status: 202 });
      }
    }
    return new Response('Not found', { status: 404 });
  },
} satisfies ExportedHandler<Bindings>;
