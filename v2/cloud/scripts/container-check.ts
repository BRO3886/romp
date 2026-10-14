import { createRequire } from 'node:module';
import { dirname } from 'node:path';
import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { z } from 'zod';
const directory = await mkdtemp(join(tmpdir(), 'romp-container-check-'));
const require = createRequire(import.meta.url);
const wrangler = join(dirname(require.resolve('wrangler/package.json')), 'bin/wrangler.js');
const runner = spawn(
  'node',
  [
    wrangler,
    'dev',
    '--config',
    'wrangler.integration.jsonc',
    '--local',
    '--port',
    '8793',
    '--persist-to',
    directory,
    '--log-level',
    'error',
  ],
  { stdio: ['ignore', 'pipe', 'pipe'] },
);
let log = '';
runner.stdout.on('data', (chunk: Buffer) => {
  log = (log + chunk.toString()).slice(-16384);
});
runner.stderr.on('data', (chunk: Buffer) => {
  log = (log + chunk.toString()).slice(-16384);
});
try {
  const deadline = Date.now() + 120_000;
  let response: Response | undefined;
  while (Date.now() < deadline) {
    try {
      response = await fetch('http://127.0.0.1:8793', { signal: AbortSignal.timeout(30_000) });
      break;
    } catch {
      if (runner.exitCode !== null) throw new Error(log);
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
  }
  if (!response?.ok)
    throw new Error(`Container check failed: ${response ? await response.text() : log}`);
  const result = z
    .object({ node: z.string().startsWith('v22.'), restored: z.literal('42') })
    .parse(await response.json());
  console.log(`PASS native container execution and snapshot restore: ${result.node}`);
} finally {
  runner.kill('SIGINT');
  if (runner.exitCode === null)
    await new Promise<void>((resolve) => runner.once('exit', () => resolve()));
  await rm(directory, { recursive: true, force: true });
}
