import test from 'node:test';
import assert from 'node:assert/strict';
import { generateKeyPairSync, createHmac } from 'node:crypto';
import { chmod, mkdir, mkdtemp, readFile, rm, stat, symlink, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { z } from 'zod';
import { randomUUID } from 'node:crypto';
import { FileCredentials } from '../../src/adapters/node/auth.js';
import { SecretStorage } from '../../src/adapters/node/secret-storage.js';
import { SubscriptionImport } from '../src/subscription-schema.js';
import { pendingSubscription, subscriptionDirectory } from '../src/setup/subscription.js';
import { main } from '../src/setup/cli.js';
import { loadProfile, profilePath } from '../src/setup/profile.js';
import { runWrangler, type Runner } from '../src/setup/deployment.js';

const root = resolve('.');
process.env['ROMP_SECRET_STORAGE'] = 'file';
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
  let session: SubscriptionImport | undefined;
  const imports: SubscriptionImport[] = [];
  const revocations: string[] = [];
  let loseImportResponse = false;
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
    if (url.hostname === 'auth.openai.com') {
      if (url.pathname === '/.well-known/openid-configuration')
        return response({ revocation_endpoint: 'https://auth.openai.com/oauth/revoke' });
      assert.equal(url.pathname, '/oauth/revoke');
      revocations.push(new URLSearchParams(requestBody).get('token') ?? '');
      return new Response(null, { status: 200 });
    }
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
    if (url.pathname === '/subscription') {
      if (method === 'PUT') {
        const value = SubscriptionImport.parse(JSON.parse(requestBody) as unknown);
        imports.push(value);
        if (session?.sessionId !== value.sessionId) session = value;
        if (loseImportResponse) throw new Error('Injected lost upload response');
      }
      if (method === 'DELETE') {
        session = undefined;
        return response({ configured: false, requiresLogin: true, remoteRevocation: true });
      }
      return response({
        configured: !!session,
        requiresLogin: !session,
        ...(session ? { sessionId: session.sessionId, hostId: session.hostId } : {}),
      });
    }
    if (url.pathname === '/doctor')
      return doctorFailure
        ? new Response(null, { status: 422 })
        : response({
            ready: true,
            openaiAuth: last.vars['OPENAI_AUTH'] ?? 'api-key',
            repositories: ['test/repo'],
            concurrency: 1,
          });
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
  const env: NodeJS.ProcessEnv = {
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
      login: async (action, provider, directory, agentName) => {
        assert.equal(action, 'login');
        assert.equal(provider, 'openai');
        assert.equal(agentName, 'Romp Cloud');
        let id: string;
        try {
          id = await readFile(join(directory, 'device-id'), 'utf8');
        } catch {
          id = randomUUID();
          await writeFile(join(directory, 'device-id'), id, { mode: 0o600 });
        }
        await new FileCredentials(directory).modify('openai', () =>
          Promise.resolve({
            type: 'oauth',
            access: 'cloud-access-token',
            refresh: 'cloud-refresh-token',
            expires: Date.now() + 3_600_000,
            clientId: 'cloud-client',
            scopes: ['chatgpt.tokens.use.direct', 'offline_access'],
          }),
        );
      },
    });
  return {
    directory,
    env,
    imports,
    revocations,
    loseImportResponse: (v: boolean) => {
      loseImportResponse = v;
    },
    remoteSession: () => session,
    args,
    run,
    outputs,
    calls,
    fetcher,
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
await test('first setup reports all missing deployment settings without provisioning', async () => {
  const f = await fixture();
  try {
    await assert.rejects(f.run(['setup', '--repo', 'test/repo']), (error: unknown) => {
      assert.ok(error instanceof Error);
      for (const flag of ['--account', '--name', '--app-id', '--private-key'])
        assert.ok(error.message.includes(flag), error.message);
      assert.match(error.message, /cloud setup --help/);
      assert.doesNotMatch(error.message, /invalid_type|expected.*string|"path"/i);
      return true;
    });
    assert.equal(f.calls.length, 0);
    assert.equal(f.deployments.length, 0);
    assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
  } finally {
    await f.cleanup();
  }
});
await test('first setup discovers GitHub origin and repository-root configuration from a subdirectory', async () => {
  for (const origin of [
    'git@github.com:test/repo.git',
    'https://github.com/test/repo.git',
    'ssh://git@github.com/test/repo.git',
  ]) {
    const f = await fixture();
    try {
      await exec('git', ['init', f.directory]);
      await exec('git', ['-C', f.directory, 'remote', 'add', 'origin', origin]);
      const nested = join(f.directory, 'nested');
      await mkdir(nested);
      const args = f.args
        .slice(0, -2)
        .map((arg) => (arg === 'key.pem' ? join(f.directory, 'key.pem') : arg));
      const outputs: unknown[] = [];
      await main([...args, '--dry-run'], {
        cwd: nested,
        home: f.directory,
        root,
        env: f.env,
        fetch: f.fetcher,
        runner: f.runner,
        output: (value) => outputs.push(value),
      });
      assert.equal(f.deployments.length, 1);
      const repositories = z
        .array(z.object({ repo: z.string() }))
        .parse(JSON.parse(f.deployments[0]?.vars['REPOSITORIES'] ?? '[]') as unknown);
      assert.deepEqual(
        repositories.map((r) => r.repo),
        ['test/repo'],
      );
      assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
    } finally {
      await f.cleanup();
    }
  }
});
await test('repository detection rejects missing or unrelated origins and explicit repos override discovery', async () => {
  const f = await fixture();
  try {
    await exec('git', ['init', f.directory]);
    const detected = [...f.args.slice(0, -2), '--dry-run'];
    await assert.rejects(f.run(detected), /Cannot detect.*--repo OWNER\/REPO/);
    await exec('git', [
      '-C',
      f.directory,
      'remote',
      'add',
      'origin',
      'git@gitlab.com:other/repo.git',
    ]);
    await assert.rejects(f.run(detected), /Cannot detect.*--repo OWNER\/REPO/);
    assert.equal(f.calls.length, 0);
    assert.equal(f.deployments.length, 0);
    await f.run([...f.args, '--dry-run']);
    assert.equal(f.deployments.length, 1);
    const repositories = z
      .array(z.object({ repo: z.string() }))
      .parse(JSON.parse(f.deployments[0]?.vars['REPOSITORIES'] ?? '[]') as unknown);
    assert.deepEqual(
      repositories.map((r) => r.repo),
      ['test/repo'],
    );
  } finally {
    await f.cleanup();
  }
});
await test('profile selection works before or after commands and cloud login reports its transfer path', async () => {
  const f = await fixture();
  try {
    const file = profilePath(f.directory, 'personal');
    await f.run(['--profile', 'personal', 'doctor']).then(
      () => assert.fail('An undeployed profile must fail doctor'),
      (error: unknown) => {
        assert.ok(error instanceof Error);
        assert.ok(error.message.includes(file));
        assert.match(error.message, /login alone does not create a deployment profile/);
      },
    );
    await f.run(['login', '--profile', 'personal']);
    assert.ok(
      String(f.outputs.at(-1)).includes(join(subscriptionDirectory(file), 'transfer.json')),
    );
    assert.equal(await loadProfile(file), undefined);
    await f.run(['--profile', 'personal', ...f.args, '--openai-auth', 'subscription']);
    assert.ok(await loadProfile(file));
    await f.run(['status', '--profile', 'personal']);
    assert.deepEqual(f.outputs.at(-1), { concurrency: 1, paused: false, runs: [] });
    assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
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
    await assert.rejects(f.run(['pause', '--concurrency', '3']), /unknown option '--concurrency'/);
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
      assert.match(result.stdout, /setup \[options\]/);
      await exec('git', ['init', f.directory]);
      await exec('git', [
        '-C',
        f.directory,
        'remote',
        'add',
        'origin',
        'git@github.com:test/repo.git',
      ]);
      await f.run([...f.args.slice(0, -2), '--dry-run'], async (args, cwd, env) => {
        assert.ok(args.includes('--dry-run'));
        await runWrangler(args, cwd, { ...process.env, ...env });
      });
      assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
    } finally {
      await f.cleanup();
    }
  },
);

await test('subscription setup transfers once, removes the local renewable copy, and preserves cloud rotation on redeploy', async () => {
  const f = await fixture();
  try {
    delete f.env['OPENAI_API_KEY'];
    await assert.rejects(f.run([...f.args, '--openai-auth', 'subscription']), /cloud login/);
    assert.equal(f.deployments.length, 0);
    await f.run(['login']);
    const directory = subscriptionDirectory(profilePath(f.directory, 'default'));
    const pending = await pendingSubscription(directory);
    assert.ok(pending);
    assert.equal((await stat(join(directory, 'transfer.json'))).mode & 0o777, 0o600);
    assert.deepEqual(await new FileCredentials(directory).list(), []);
    await f.run(
      [...f.args, '--openai-auth', 'subscription', '--dry-run'],
      async (args, cwd, env) => {
        assert.ok(args.includes('--dry-run'));
        await f.runner(args, cwd, env);
        await runWrangler(args, cwd, env);
      },
    );
    assert.equal(await loadProfile(profilePath(f.directory, 'default')), undefined);
    assert.equal(f.imports.length, 0);
    assert.deepEqual(await pendingSubscription(directory), pending);
    f.env['OPENAI_API_KEY'] = 'sk-must-not-upload';
    await f.run([...f.args, '--openai-auth', 'subscription']);
    assert.equal(f.imports.length, 1);
    assert.equal(await pendingSubscription(directory), undefined);
    const profile = await loadProfile(profilePath(f.directory, 'default'));
    assert.equal(profile?.openaiAuth, 'subscription');
    assert.match(profile?.secrets.SUBSCRIPTION_KEY ?? '', /^[a-f0-9]{64}$/);
    assert.equal(profile?.secrets.OPENAI_API_KEY, undefined);
    const session = f.remoteSession();
    assert.ok(session);
    session.credential.refresh = 'cloud-rotated-token';
    await f.run(['setup']);
    await f.run(['doctor']);
    assert.equal(f.imports.length, 1);
    assert.equal(f.remoteSession()?.credential.refresh, 'cloud-rotated-token');
    for (const deployment of f.deployments) {
      const text = JSON.stringify(deployment);
      for (const value of ['cloud-refresh-token', 'cloud-access-token', 'sk-must-not-upload'])
        assert.ok(!text.includes(value));
    }
    assert.ok(!JSON.stringify(f.outputs).includes('cloud-refresh-token'));
    await f.run(['login']);
    assert.equal(f.imports.length, 2);
    assert.equal(f.remoteSession()?.hostId, pending.hostId);
    await f.run(['logout']);
    assert.equal(f.remoteSession(), undefined);
    await f.run(['setup', '--openai-auth', 'api-key']);
    assert.equal(
      (await loadProfile(profilePath(f.directory, 'default')))?.secrets.SUBSCRIPTION_KEY,
      profile?.secrets.SUBSCRIPTION_KEY,
    );
  } finally {
    await f.cleanup();
  }
});
await test('a lost subscription upload response keeps the exact pending session for safe retry before webhook activation', async () => {
  const f = await fixture();
  try {
    delete f.env['OPENAI_API_KEY'];
    await f.run(['login']);
    const directory = subscriptionDirectory(profilePath(f.directory, 'default'));
    const pending = await pendingSubscription(directory);
    f.loseImportResponse(true);
    await assert.rejects(
      f.run([...f.args, '--openai-auth', 'subscription']),
      /lost upload response/,
    );
    assert.ok(!f.calls.some((call) => call.method === 'PATCH'));
    assert.deepEqual(await pendingSubscription(directory), pending);
    f.loseImportResponse(false);
    await f.run(['setup']);
    assert.equal(f.imports.length, 2);
    assert.equal(f.imports[0]?.sessionId, f.imports[1]?.sessionId);
    assert.equal(await pendingSubscription(directory), undefined);
    assert.ok(
      f.calls.findIndex((call) => call.path === '/subscription' && call.method === 'PUT') <
        f.calls.findIndex((call) => call.method === 'PATCH'),
    );
  } finally {
    await f.cleanup();
  }
});

await test('cloud logout before deployment revokes the pending session and retains only the stable host identity', async () => {
  const f = await fixture();
  try {
    await f.run(['login']);
    const directory = subscriptionDirectory(profilePath(f.directory, 'default'));
    const hostId = await readFile(join(directory, 'device-id'), 'utf8');
    await f.run(['logout']);
    assert.deepEqual(f.revocations, ['cloud-refresh-token']);
    assert.equal(await pendingSubscription(directory), undefined);
    assert.deepEqual(await new FileCredentials(directory).list(), []);
    assert.equal(await readFile(join(directory, 'device-id'), 'utf8'), hostId);
    assert.equal(f.deployments.length, 0);
  } finally {
    await f.cleanup();
  }
});

await test(
  'native credential vault protects cloud transfer and deployment secrets through upload recovery',
  {
    skip:
      process.platform !== 'darwin' &&
      process.env['ROMP_TEST_NATIVE_VAULT'] !== '1' &&
      'coverage gap: native credential vault not runnable on this host',
  },
  async () => {
    process.env['ROMP_SECRET_STORAGE'] = 'keyring';
    const f = await fixture();
    const file = profilePath(f.directory, 'default');
    const directory = subscriptionDirectory(file);
    try {
      delete f.env['OPENAI_API_KEY'];
      await f.run(['login']);
      assert.equal(
        (await readFile(join(directory, 'transfer.json'), 'utf8')).includes('cloud-refresh-token'),
        false,
      );
      const pending = await pendingSubscription(directory);
      assert.ok(pending);
      f.loseImportResponse(true);
      await assert.rejects(
        f.run([...f.args, '--openai-auth', 'subscription']),
        /lost upload response/,
      );
      assert.equal((await readFile(file, 'utf8')).includes('GITHUB_PRIVATE_KEY'), false);
      const legacyProfile = await loadProfile(file);
      assert.ok(legacyProfile);
      await new SecretStorage(file).delete();
      await writeFile(file, JSON.stringify(legacyProfile), { mode: 0o600 });
      assert.deepEqual(await loadProfile(file), legacyProfile);
      assert.equal((await readFile(file, 'utf8')).includes('GITHUB_PRIVATE_KEY'), false);
      assert.deepEqual(await pendingSubscription(directory), pending);
      f.loseImportResponse(false);
      await f.run(['setup']);
      assert.equal(f.imports[1]?.sessionId, pending.sessionId);
      assert.equal(await pendingSubscription(directory), undefined);
      await f.run(['doctor']);
      await f.run(['logout']);
    } finally {
      for (const reference of [
        file,
        join(directory, 'auth.json'),
        join(directory, 'transfer.json'),
      ]) {
        await new SecretStorage(reference, { mode: 'keyring' }).delete();
      }
      process.env['ROMP_SECRET_STORAGE'] = 'file';
      await f.cleanup();
    }
  },
);
await test('profiles written before subscription support retain API-key authentication', async () => {
  const f = await fixture();
  try {
    await f.run(f.args);
    const file = profilePath(f.directory, 'default');
    const original = z
      .record(z.string(), z.unknown())
      .parse(JSON.parse(await readFile(file, 'utf8')) as unknown);
    delete original['openaiAuth'];
    await writeFile(file, JSON.stringify(original), { mode: 0o600 });
    assert.equal((await loadProfile(file))?.openaiAuth, 'api-key');
    await f.run(['doctor']);
    await f.run(['setup']);
    assert.equal(f.imports.length, 0);
  } finally {
    await f.cleanup();
  }
});
