import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync, createHmac } from 'node:crypto';
import { chmod, mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { z } from 'zod';
import { main } from '../src/setup/cli.js';
import { loadProfile, profilePath } from '../src/setup/profile.js';
import { runWrangler, type Runner } from '../src/setup/deployment.js';

const root = resolve('.');
const exec = promisify(execFile);
const config = `[agent]\nprovider="openai"\nmodel="gpt-5.4"\n[verify]\ncommands=["node --version"]\n[review]\nenabled=false\n`;
const permissions = { contents: 'write', issues: 'write', pull_requests: 'write' };
async function fixture() {
  const directory = await mkdtemp(join(tmpdir(), 'romp-setup-'));
  const key = generateKeyPairSync('rsa', {
    modulusLength: 2048,
    privateKeyEncoding: { format: 'pem', type: 'pkcs8' },
    publicKeyEncoding: { format: 'pem', type: 'spki' },
  }).privateKey;
  await writeFile(join(directory, 'key.pem'), key);
  await writeFile(join(directory, 'romp-v2.toml'), config);
  const deployments: {
    args: string[];
    vars: Record<string, string>;
    secrets: Record<string, string>;
  }[] = [];
  const calls: { path: string; method: string }[] = [];
  const outputs: unknown[] = [];
  let hook = { url: '', content_type: 'json', insecure_ssl: '0' };
  let width = 1;
  let paused = false;
  let doctorFailure = false;
  let hookFailure = false;
  let denied = false;
  let labelFailure = false;
  const labels = new Set<string>();
  const runner: Runner = async (args, cwd, env) => {
    assert.equal(cwd, root);
    assert.equal(env['CLOUDFLARE_ACCOUNT_ID'], 'a'.repeat(32));
    const file = args[args.indexOf('--config') + 1];
    const secretFile = args[args.indexOf('--secrets-file') + 1];
    assert.ok(file && secretFile);
    const parsed = z
      .object({
        vars: z.record(z.string(), z.string()),
        main: z.string(),
        containers: z.array(
          z.object({ images: z.object({ runner: z.object({ dockerfile: z.string() }) }) }),
        ),
      })
      .parse(JSON.parse(await readFile(file, 'utf8')) as unknown);
    assert.equal(parsed.main, join(root, 'src/worker.ts'));
    assert.equal(parsed.containers[0]?.images.runner.dockerfile, join(root, 'Dockerfile'));
    const secrets = z
      .record(z.string(), z.string())
      .parse(JSON.parse(await readFile(secretFile, 'utf8')) as unknown);
    assert.equal((await stat(secretFile)).mode & 0o777, 0o600);
    deployments.push({ args, vars: parsed.vars, secrets });
  };
  const fetcher: typeof fetch = async (input, init) => {
    const url = new URL(
      typeof input === 'string' ? input : input instanceof URL ? input.href : input.url,
    );
    const requestBody = init?.body === undefined ? '' : await new Response(init.body).text();
    const method = init?.method ?? 'GET';
    calls.push({ path: url.pathname, method });
    const response = (body: unknown) => Response.json(body);
    if (url.hostname === 'api.cloudflare.com') {
      assert.equal(new Headers(init?.headers).get('authorization'), 'Bearer cloud-token');
      return response({
        success: true,
        result: url.pathname.endsWith('/subdomain') ? { subdomain: 'test' } : [],
      });
    }
    if (url.hostname === 'api.github.com') {
      if (new Headers(init?.headers).get('authorization') !== 'Bearer installation-token') {
        const jwt = new Headers(init?.headers).get('authorization')?.split('.')[1];
        assert.ok(jwt);
        const claims = z
          .object({ exp: z.number() })
          .parse(JSON.parse(atob(jwt.replace(/-/g, '+').replace(/_/g, '/'))) as unknown);
        assert.ok(
          claims.exp * 1000 > Date.now(),
          'GitHub App JWT must not expire during an image build',
        );
      }
      if (url.pathname === '/app') return response({ id: 123, events: ['issues'], permissions });
      if (url.pathname === '/app/hook/config') {
        if (method === 'PATCH') {
          hook = z
            .object({ url: z.string(), content_type: z.string(), insecure_ssl: z.string() })
            .parse(JSON.parse(requestBody) as unknown);
          if (hookFailure) return new Response(null, { status: 503 });
        }
        return response(hook);
      }
      if (denied) return new Response(null, { status: 403 });
      if (url.pathname.endsWith('/installation'))
        return response({ id: 456, permissions, suspended_at: null });
      if (url.pathname.endsWith('/access_tokens')) {
        assert.deepEqual(JSON.parse(requestBody) as unknown, {
          repositories: ['repo'],
          permissions,
        });
        return response({ token: 'installation-token' });
      }
      if (url.pathname === '/repos/test/repo') {
        assert.equal(new Headers(init?.headers).get('authorization'), 'Bearer installation-token');
        return response({ full_name: 'test/repo', has_issues: true });
      }
      if (url.pathname.startsWith('/repos/test/repo/labels/'))
        return labels.has(decodeURIComponent(url.pathname.split('/').at(-1) ?? ''))
          ? response({})
          : new Response(null, { status: 404 });
      if (url.pathname === '/repos/test/repo/labels' && method === 'POST') {
        if (labelFailure) return new Response(null, { status: 403 });
        labels.add(z.object({ name: z.string() }).parse(JSON.parse(requestBody) as unknown).name);
        return response({});
      }
    }
    assert.equal(url.origin, 'https://romp-test.test.workers.dev');
    const last = deployments.at(-1);
    assert.ok(last);
    if (url.pathname === '/webhook') {
      assert.equal(
        new Headers(init?.headers).get('x-hub-signature-256'),
        `sha256=${createHmac('sha256', last.secrets['GITHUB_WEBHOOK_SECRET'] ?? '')
          .update('{}')
          .digest('hex')}`,
      );
      return new Response('pong');
    }
    assert.equal(
      new Headers(init?.headers).get('authorization'),
      `Bearer ${last.secrets['ADMIN_TOKEN']}`,
    );
    if (url.pathname === '/doctor')
      return doctorFailure
        ? new Response(null, { status: 422 })
        : response({ ready: true, repositories: ['test/repo'], concurrency: 1 });
    if (url.pathname === '/concurrency')
      width = z
        .object({ concurrency: z.number() })
        .parse(JSON.parse(requestBody) as unknown).concurrency;
    if (url.pathname === '/pause') paused = true;
    if (url.pathname === '/resume') paused = false;
    return response({ concurrency: width, paused, runs: [] });
  };
  const args = [
    'setup',
    '--account',
    'a'.repeat(32),
    '--name',
    'romp-test',
    '--app-id',
    '123',
    '--private-key',
    'key.pem',
    '--repo',
    'test/repo',
  ];
  const env = {
    ...process.env,
    CLOUDFLARE_API_TOKEN: 'cloud-token',
    OPENAI_API_KEY: 'model-secret',
  };
  const run = (args: string[], execute: Runner = runner) =>
    main(args, {
      home: directory,
      cwd: directory,
      root,
      env,
      runner: execute,
      fetch: fetcher,
      output: (v) => outputs.push(v),
    });
  return {
    directory,
    args,
    run,
    outputs,
    calls,
    deployments,
    runner,
    setHook: (url: string) => {
      hook.url = url;
    },
    failDoctor: (v: boolean) => {
      doctorFailure = v;
    },
    failHook: (v: boolean) => {
      hookFailure = v;
    },
    denyRepository: () => {
      denied = true;
    },
    labels,
    failLabels: (v: boolean) => {
      labelFailure = v;
    },
    cleanup: () => rm(directory, { recursive: true, force: true }),
  };
}
await test('dry-run resolves installation IDs and deploy inputs without saving secrets or changing the hook', async () => {
  const f = await fixture();
  try {
    await f.run([...f.args, '--dry-run']);
    assert.equal(f.deployments.length, 1);
    assert.ok(f.deployments[0]?.args.includes('--dry-run'));
    const repositories = z
      .array(z.object({ installationId: z.number() }))
      .parse(JSON.parse(f.deployments[0]?.vars['REPOSITORIES'] ?? '') as unknown);
    assert.equal(repositories[0]?.installationId, 456);
    assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
    assert.ok(!f.calls.some((c) => c.method === 'PATCH'));
    assert.ok(!JSON.stringify(f.outputs).includes('model-secret'));
  } finally {
    await f.cleanup();
  }
});
await test('setup creates required labels before connecting the webhook and retries label failures', async () => {
  const f = await fixture();
  try {
    f.failLabels(true);
    await assert.rejects(f.run(f.args), /Cannot create repository label/);
    assert.ok(!f.calls.some((c) => c.method === 'PATCH'));
    f.failLabels(false);
    await f.run(['setup']);
    assert.deepEqual([...f.labels].sort(), [
      'romp:v2',
      'romp:v2:blocked',
      'romp:v2:changes-requested',
      'romp:v2:claimed',
    ]);
    const count = f.calls.filter(
      (c) => c.path === '/repos/test/repo/labels' && c.method === 'POST',
    ).length;
    await f.run(['setup']);
    assert.equal(
      f.calls.filter((c) => c.path === '/repos/test/repo/labels' && c.method === 'POST').length,
      count,
    );
  } finally {
    await f.cleanup();
  }
});
await test('long deployment refreshes App authorization before connecting the webhook', async (t) => {
  const f = await fixture();
  let now = Date.now();
  t.mock.method(Date, 'now', () => now);
  try {
    await f.run(f.args, async (args, cwd, env) => {
      await f.runner(args, cwd, env);
      now += 600_000;
    });
  } finally {
    t.mock.restoreAll();
    await f.cleanup();
  }
});
await test('setup checkpoints secrets before deployment and repairs an ambiguous hook update with the same credentials', async () => {
  const f = await fixture();
  try {
    f.failHook(true);
    await assert.rejects(f.run(f.args), /HTTP 503/);
    const file = profilePath(f.directory, 'default');
    const first = await loadProfile(file);
    assert.ok(first);
    assert.equal((await stat(file)).mode & 0o777, 0o600);
    assert.equal((await stat(join(f.directory, '.romp-v2/cloud'))).mode & 0o777, 0o700);
    const patchIndex = f.calls.findIndex((c) => c.method === 'PATCH');
    assert.ok(f.calls.slice(0, patchIndex).some((c) => c.path === '/doctor'));
    assert.ok(f.calls.slice(0, patchIndex).some((c) => c.path === '/webhook'));
    f.failHook(false);
    await f.run(['setup']);
    assert.deepEqual((await loadProfile(file))?.secrets, first.secrets);
    assert.equal(f.deployments.length, 4);
    await f.run(['doctor']);
  } finally {
    await f.cleanup();
  }
});
await test('failed deployment readiness never connects a webhook and can be retried', async () => {
  const f = await fixture();
  try {
    f.failDoctor(true);
    await assert.rejects(f.run(f.args), /HTTP 422/);
    assert.ok(!f.calls.some((c) => c.method === 'PATCH'));
    f.failDoctor(false);
    await f.run(['setup']);
  } finally {
    await f.cleanup();
  }
});
await test('invalid model, inaccessible repository, and unrelated App webhook stop before deployment', async () => {
  for (const failure of ['model', 'repository', 'webhook']) {
    const f = await fixture();
    try {
      if (failure === 'model')
        await writeFile(
          join(f.directory, 'romp-v2.toml'),
          config.replace('gpt-5.4', 'missing-model'),
        );
      if (failure === 'repository') f.denyRepository();
      if (failure === 'webhook') f.setHook('https://other.example/webhook');
      await assert.rejects(
        f.run(f.args),
        failure === 'model'
          ? /Configured model/
          : failure === 'repository'
            ? /HTTP 403/
            : /dedicated App/,
      );
      assert.equal(f.deployments.length, 0);
      assert.ok(!f.calls.some((c) => c.method === 'PATCH'));
      if (failure === 'model') assert.equal(f.calls.length, 0);
    } finally {
      await f.cleanup();
    }
  }
});
await test('administration uses saved authentication, persists width, and rejects unsupported options', async () => {
  const f = await fixture();
  try {
    await f.run(f.args);
    await f.run(['pause']);
    assert.deepEqual(f.outputs.at(-1), { concurrency: 1, paused: true, runs: [] });
    await f.run(['configure', '--concurrency', '3']);
    assert.equal((await loadProfile(profilePath(f.directory, 'default')))?.concurrency, 3);
    await f.run(['doctor']);
    await f.run(['resume']);
    await f.run(['status']);
    assert.deepEqual(f.outputs.at(-1), { concurrency: 3, paused: false, runs: [] });
    await assert.rejects(f.run(['configure', '--concurrency', '0']), /1 to 32/);
    await assert.rejects(f.run(['pause', '--concurrency', '3']), /Unsupported option/);
    await assert.rejects(f.run(['setup', '--account', 'b'.repeat(32)]), /fixed for a profile/);
  } finally {
    await f.cleanup();
  }
});
await test('profiles reject permissive files and symbolic links', async () => {
  const f = await fixture();
  try {
    await f.run(f.args);
    const file = profilePath(f.directory, 'default');
    await chmod(file, 0o644);
    await assert.rejects(f.run(['status']), /private file/);
    await symlink(file, profilePath(f.directory, 'linked'));
    await assert.rejects(f.run(['status', '--profile', 'linked']), /ELOOP/);
  } finally {
    await f.cleanup();
  }
});
await test(
  'compiled root CLI and real Wrangler resolve generated deployment configuration without provisioning',
  { timeout: 360_000 },
  async () => {
    const f = await fixture();
    try {
      const result = await exec(process.execPath, [resolve('../dist/cli.js'), 'cloud', '--help'], {
        cwd: f.directory,
        env: { ...process.env, OPENAI_API_KEY: 'test-key' },
        timeout: 30_000,
        maxBuffer: 4 * 1024 * 1024,
      });
      assert.match(result.stdout, /setup --account/);
      await f.run([...f.args, '--dry-run'], async (args, cwd, env) => {
        assert.ok(args.includes('--dry-run'));
        await runWrangler(args, cwd, { ...process.env, ...env });
      });
      assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
    } finally {
      await f.cleanup();
    }
  },
);
