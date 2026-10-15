import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { command } from '../src/adapters/node/process.js';
import { z } from 'zod';
import { parseConfig } from '../src/domain/config.js';

const cli = resolve('dist/cli.js');
await test('command help and usage errors require no runtime or state', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'romp-cli-'));
  const state = join(directory, 'state');
  const guard = join(directory, 'guard.mjs');
  try {
    await writeFile(
      guard,
      `import { registerHooks } from 'node:module';
registerHooks({ resolve(specifier, context, next) {
  if (specifier === 'node:sqlite') throw new Error('CLI imported SQLite');
  return next(specifier, context);
} });`,
    );
    const run = (args: string[]) =>
      command(process.execPath, [cli, ...args], directory, undefined, {
        allowFailure: true,
        env: {
          ...process.env,
          ROMP_V2_STATE: state,
          NODE_OPTIONS: `--import=${pathToFileURL(guard).href}`,
        },
      });
    for (const [args, expected] of [
      [['--help'], /Romp v2/],
      [['daemon', 'start', '--help'], /--width/],
      [['cloud', 'setup', '--help'], /--openai-auth/],
      [['help', 'run'], /--issue/],
    ] as const) {
      const result = await run([...args]);
      assert.equal(result.code, 0, result.stderr);
      assert.match(result.stdout, expected);
      assert.equal(result.stderr, '');
    }
    const version = await run(['--version']);
    assert.equal(version.code, 0, version.stderr);
    const manifest = z
      .object({ version: z.string() })
      .parse(JSON.parse(await readFile('package.json', 'utf8')) as unknown);
    assert.equal(version.stdout.trim(), manifest.version);
    for (const args of [
      ['init', '--provider', 'openai', '--model', 'gpt-5.4'],
      ['run'],
      ['run', '--issue', '0'],
      ['run', '--issue', '1.5'],
      ['status', '--issue', '1'],
      ['status', 'unexpected'],
      ['daemon', 'start', '--width', '0'],
      ['cloud', 'pause', '--concurrency', '3'],
      ['cloud', 'login', 'unexpected'],
      ['cloud', 'configure'],
    ]) {
      const result = await run(args);
      assert.notEqual(result.code, 0, args.join(' '));
      assert.doesNotMatch(result.stderr, /SQLite|ENOENT|ECONNREFUSED/);
      assert.match(result.stderr, /error:/i);
    }
    await assert.rejects(stat(state), { code: 'ENOENT' });
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
await test('init keeps repeated verification commands literal and refuses to overwrite configuration', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'romp-cli-init-'));
  try {
    await command('git', ['init', directory], directory);
    const args = [
      'init',
      '--provider',
      'openai',
      '--model',
      'gpt-5.4',
      '--verify',
      'npm run check',
      '--verify',
      'printf "%s" "literal $(false)"',
    ];
    await command(process.execPath, [cli, ...args], directory);
    const file = join(directory, 'romp-v2.toml');
    const contents = await readFile(file, 'utf8');
    assert.deepEqual(parseConfig(contents).verify, [
      'npm run check',
      'printf "%s" "literal $(false)"',
    ]);
    await assert.rejects(command(process.execPath, [cli, ...args], directory), /EEXIST/);
    assert.equal(await readFile(file, 'utf8'), contents);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
