#!/usr/bin/env node
import { writeFile } from 'node:fs/promises';
import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { spawn } from 'node:child_process';
import { parseArgs } from 'node:util';
import { stringify } from 'smol-toml';
import { preflight, providerModels } from './composition/node.js';
import { LocalRepositories } from './adapters/node/repositories.js';
import { createClient } from './adapters/node/client.js';
import { GitHubCLI } from './adapters/node/github.js';
import { serve } from './adapters/node/daemon.js';
import { stateDirectory } from './adapters/node/paths.js';
import { authenticate } from './adapters/node/login.js';
import { message } from './shared/errors.js';

const help = `Romp v2 (experimental, independent of v1)

  init --provider NAME --model ID --verify COMMAND [--verify COMMAND]
  models [--provider NAME]
  login --provider NAME | logout --provider NAME
  doctor
  cloud setup | cloud doctor | cloud status
  cloud configure --concurrency N | cloud pause | cloud resume
  daemon start [--width N] | daemon stop | daemon status
  run --issue N [--detach]
  watch | unwatch
  status | history
  job ID | wait ID | logs ID | cancel ID
  steer ID MESSAGE

Run daemon start in a persistent terminal. Disconnecting clients leaves work running.
ROMP_V2_STATE overrides the state directory. Config: romp-v2.toml.
`;
export async function main(args: string[]) {
  if (args[0] === 'cloud') {
    const file = fileURLToPath(new URL('../cloud/dist/cloud/src/setup/cli.js', import.meta.url));
    await new Promise<void>((resolve, reject) => {
      const child = spawn(process.execPath, [file, ...args.slice(1)], { stdio: 'inherit' });
      child.once('error', reject);
      child.once('exit', (code) => {
        process.exitCode = code ?? 1;
        resolve();
      });
    });
    return;
  }
  const { values, positionals } = parseArgs({
    args,
    allowPositionals: true,
    options: {
      provider: { type: 'string' },
      model: { type: 'string' },
      verify: { type: 'string', multiple: true },
      issue: { type: 'string', short: 'i' },
      width: { type: 'string' },
      detach: { type: 'boolean' },
      help: { type: 'boolean', short: 'h' },
    },
  });
  const [action, id, ...rest] = positionals;
  const client = createClient(stateDirectory());
  if (!action || values.help) {
    process.stdout.write(help);
    return;
  }
  const print = (data: unknown) => process.stdout.write(JSON.stringify(data, null, 2) + '\n');
  if (action === 'models') {
    print(
      providerModels()
        .getModels(values.provider)
        .map((m) => ({ provider: m.provider, model: m.id })),
    );
    return;
  }
  if (action === 'login' || action === 'logout') {
    if (!values.provider) throw new Error(`${action} requires --provider`);
    await authenticate(action, values.provider, stateDirectory());
    return;
  }

  if (action === 'init') {
    if (!values.provider || !values.model || !values.verify?.length)
      throw new Error('init requires --provider, --model, and explicit --verify commands');
    const root = await new LocalRepositories().root(process.cwd());
    await writeFile(
      join(root, 'romp-v2.toml'),
      stringify({
        label: 'romp:v2',
        claimed_label: 'romp:v2:claimed',
        blocked_label: 'romp:v2:blocked',
        changes_requested_label: 'romp:v2:changes-requested',
        timeout_ms: 1_800_000,
        verify: { commands: values.verify },
        agent: { provider: values.provider, model: values.model },
        review: { enabled: true, max_fix_rounds: 2 },
      }),
      { flag: 'wx' },
    );
    process.stdout.write('Created romp-v2.toml. Create its GitHub labels before running jobs.\n');
    return;
  }
  if (action === 'doctor') {
    const result = await preflight(
      process.cwd(),
      new GitHubCLI(process.cwd()),
      providerModels(stateDirectory()),
    );
    print({ ...result, stateDirectory: stateDirectory(), provisioned: false });
    return;
  }
  if (action === 'daemon') {
    if (id === 'start') {
      const controller = new AbortController();
      const stop = () => controller.abort();
      process.once('SIGINT', stop);
      process.once('SIGTERM', stop);
      try {
        await serve(stateDirectory(), {
          width: Number(values.width ?? 1),
          signal: controller.signal,
        });
      } finally {
        process.removeListener('SIGINT', stop);
        process.removeListener('SIGTERM', stop);
      }
      return;
    }
    if (id === 'stop') {
      print(await client.stop({}));
      return;
    }
    if (id === 'status') {
      print(await client.ping({}));
      return;
    }
    throw new Error('Expected daemon start, stop, or status');
  }
  if (action === 'run') {
    const job = await client.run({ root: process.cwd(), issue: Number(values.issue) });
    print(job);
    if (!values.detach) {
      const result = await client.wait({ id: job.id });
      print(result);
      if (result.outcome !== 'done') process.exitCode = 1;
    }
    return;
  }
  if (action === 'watch' || action === 'unwatch') {
    print(await client[action]({ root: process.cwd() }));
    return;
  }
  if (action === 'status' || action === 'history') {
    const state = await client.state({});
    print(
      Object.values(state.jobs).filter((j) =>
        action === 'history' ? Boolean(j.outcome) : !j.outcome,
      ),
    );
    return;
  }
  if (
    action === 'job' ||
    action === 'wait' ||
    action === 'logs' ||
    action === 'cancel' ||
    action === 'steer'
  ) {
    if (!id) throw new Error(`${action} requires a job ID`);
    if (action === 'steer') {
      print(await client.steer({ id, content: rest.join(' ') }));
      return;
    }
    if (action === 'wait') {
      const result = await client.wait({ id });
      print(result);
      if (result.outcome !== 'done') process.exitCode = 1;
      return;
    }
    print(await client[action]({ id }));
    return;
  }
  throw new Error(`Unknown command ${action}`);
}
if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(message(error) + '\n');
    process.exitCode = 1;
  });
}
