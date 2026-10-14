import test from 'node:test';
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { generateKeyPairSync } from 'node:crypto';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { parse } from 'jsonc-parser';
import { z } from 'zod';
import { config } from './fixture.js';
const exec = promisify(execFile);
await test('real Wrangler configuration resolves without provisioning and rejects unknown models', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'romp-preflight-'));
  const source = z
    .object({ vars: z.record(z.string(), z.string()) })
    .passthrough()
    .parse(parse(await readFile('wrangler.jsonc', 'utf8')) as unknown);
  const { privateKey } = generateKeyPairSync('rsa', {
    modulusLength: 2048,
    privateKeyEncoding: { format: 'pem', type: 'pkcs8' },
    publicKeyEncoding: { format: 'pem', type: 'spki' },
  });
  const file = join(directory, 'wrangler.jsonc');
  const env = {
    ...process.env,
    GITHUB_PRIVATE_KEY: privateKey,
    GITHUB_WEBHOOK_SECRET: 'w'.repeat(32),
    ADMIN_TOKEN: 'a'.repeat(32),
    OPENAI_API_KEY: 'test-key',
  };
  const run = () =>
    exec(process.execPath, ['--import', 'tsx', 'scripts/preflight.ts', file], { env });
  try {
    source.vars['REPOSITORIES'] = JSON.stringify([config]);
    source.vars['GITHUB_APP_ID'] = '123';
    await writeFile(file, JSON.stringify(source));
    const result = await run();
    assert.deepEqual(JSON.parse(result.stdout) as unknown, {
      ready: true,
      concurrency: 1,
      repositories: ['test/repo'],
      provisions: false,
    });
    source.vars['REPOSITORIES'] = JSON.stringify([
      {
        ...config,
        config: { ...config.config, model: { provider: 'openai', modelId: 'does-not-exist' } },
      },
    ]);
    await writeFile(file, JSON.stringify(source));
    await assert.rejects(run(), /Configured model or authentication is unavailable/);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
