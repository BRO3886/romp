import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { createHash } from 'node:crypto';
import { mkdtemp, mkdir, writeFile, readFile, readlink, rm } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
const exec = promisify(execFile);
const version = '2.0.0-experimental.2';
const tag = `v${version}`;
const archive = `romp-v2-${version}.tar.gz`;
async function fixture() {
  const root = await mkdtemp(join(tmpdir(), 'romp-install-test-'));
  const prefix = join(root, "local space's");
  const source = join(root, 'source/romp-v2');
  const shim = join(root, 'shim');
  await mkdir(join(source, 'dist'), { recursive: true });
  await mkdir(shim);
  await writeFile(join(source, 'package.json'), JSON.stringify({ version }));
  await writeFile(join(source, 'release.json'), JSON.stringify({ version }));
  await writeFile(
    join(source, 'dist/cli.js'),
    'console.log(JSON.stringify(process.argv.slice(2)))',
  );
  const file = join(root, archive);
  await exec('tar', ['-czf', file, '-C', join(root, 'source'), 'romp-v2']);
  const bytes = await readFile(file);
  const checksum = `${createHash('sha256').update(bytes).digest('hex')}  ${archive}\n`;
  await writeFile(
    join(shim, 'npm'),
    '#!/bin/sh\nprintf \'%s\\n\' "$@" > "$ROMP_TEST_NPM_ARGS"\nexit "${ROMP_TEST_NPM_FAILURE:-0}"\n',
    { mode: 0o755 },
  );
  let corrupt = false;
  let requests = 0;
  const server = createServer((req, res) => {
    requests++;
    if (req.url === `/${tag}/SHA256SUMS`) res.end(checksum);
    else if (req.url === `/${tag}/${archive}`) res.end(corrupt ? 'corrupt archive' : bytes);
    else {
      res.writeHead(404);
      res.end();
    }
  });
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r));
  const address = server.address();
  assert.ok(address && typeof address !== 'string');
  const args = [
    resolve('install.sh'),
    '--prefix',
    prefix,
    '--release-base-url',
    `http://127.0.0.1:${address.port}`,
  ];
  const env = {
    ...process.env,
    PATH: `${shim}:${process.env['PATH'] ?? ''}`,
    ROMP_TEST_NPM_ARGS: join(root, 'npm-args'),
  };
  const install = (fail = false) =>
    exec('sh', args, { env: { ...env, ROMP_TEST_NPM_FAILURE: fail ? '1' : '0' } });
  return {
    root,
    prefix,
    install,
    env,
    corrupt: () => {
      corrupt = true;
    },
    requests: () => requests,
    cleanup: async () => {
      await new Promise<void>((r, reject) =>
        server.close((error) => (error ? reject(error) : r())),
      );
      await rm(root, { recursive: true, force: true });
    },
  };
}
await test('installer activates romp on PATH, preserves literal arguments, and supports repeat installation', async () => {
  const f = await fixture();
  try {
    await f.install();
    assert.equal(await readFile(join(f.root, 'npm-args'), 'utf8'), 'ci\n--ignore-scripts\n');
    const current = join(f.prefix, 'share/romp/current');
    const first = await readlink(current);
    const result = await exec('romp', ['literal $(not-a-command)', "space's here"], {
      env: { ...f.env, PATH: `${f.prefix}/bin:${f.env.PATH}` },
    });
    assert.deepEqual(JSON.parse(result.stdout) as unknown, [
      'literal $(not-a-command)',
      "space's here",
    ]);
    await f.install();
    assert.notEqual(await readlink(current), first);
    assert.equal(await readFile(join(first, 'package.json'), 'utf8'), JSON.stringify({ version }));
  } finally {
    await f.cleanup();
  }
});
await test('checksum and dependency failures preserve the active installation and allow recovery', async () => {
  const f = await fixture();
  try {
    await assert.rejects(f.install(true));
    await f.install();
    const current = join(f.prefix, 'share/romp/current');
    const previous = await readlink(current);
    await assert.rejects(f.install(true));
    assert.equal(await readlink(current), previous);
    f.corrupt();
    await assert.rejects(f.install(), /checksum verification failed/);
    assert.equal(await readlink(current), previous);
  } finally {
    await f.cleanup();
  }
});
await test('installer refuses unrelated binaries and application data before downloading', async () => {
  for (const existing of ['bin/romp', 'share/romp/unrelated']) {
    const f = await fixture();
    try {
      const file = join(f.prefix, existing);
      await mkdir(resolve(file, '..'), { recursive: true });
      await writeFile(file, 'preserve');
      await assert.rejects(f.install(), /not managed by this installer/);
      assert.equal(await readFile(file, 'utf8'), 'preserve');
      assert.equal(f.requests(), 0);
    } finally {
      await f.cleanup();
    }
  }
});
