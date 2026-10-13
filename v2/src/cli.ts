#!/usr/bin/env node
import { readFile, writeFile } from 'node:fs/promises';
import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import { createInterface } from 'node:readline/promises';
import { join } from 'node:path';
import { parseArgs } from 'node:util';
import { stringify } from 'smol-toml';
import { preflight, providerModels } from './runtime.js';
import { repositoryRoot } from './config.js';
import { GitHubCLI } from './github.js';
import { acquireLock, rpc, serve, stateDirectory } from './daemon.js';
import { message, type Job } from './domain.js';

const help = `Romp v2 (experimental, independent of v1)

  init --provider NAME --model ID --verify COMMAND [--verify COMMAND]
  models [--provider NAME]
  login --provider NAME | logout --provider NAME
  doctor
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
  const { values, positionals } = parseArgs({ args, allowPositionals: true, options: {
    provider: { type: 'string' }, model: { type: 'string' }, verify: { type: 'string', multiple: true },
    issue: { type: 'string', short: 'i' }, width: { type: 'string' }, detach: { type: 'boolean' }, help: { type: 'boolean', short: 'h' },
  } });
  const [action, id, ...rest] = positionals;
  if (!action || values.help) { process.stdout.write(help); return; }
  const print = (data: unknown) => process.stdout.write(JSON.stringify(data, null, 2) + '\n');
  if (action === 'models') {
    print(providerModels().getModels(values.provider).map((m) => ({ provider: m.provider, model: m.id })));
    return;
  }
  if (action === 'login' || action === 'logout') {
    if (!values.provider) throw new Error(`${action} requires --provider`);
    const directory = stateDirectory();
    const unlock = await acquireLock(directory);
    const models = providerModels(directory);
    const terminal = createInterface({ input: process.stdin, output: process.stdout });
    try {
      if (action === 'logout') { await models.logout(values.provider); return; }
      const path = join(directory, 'device-id');
      let deviceId: string;
      try { deviceId = (await readFile(path, 'utf8')).trim(); }
      catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
        deviceId = randomUUID(); await writeFile(path, deviceId, { mode: 0o600, flag: 'wx' });
      }
      await models.login(values.provider, 'oauth', {
        prompt: async (prompt) => {
          if (prompt.type === 'select') {
            process.stdout.write(prompt.options.map((o, i) => `${i + 1}. ${o.label}`).join('\n') + '\n');
            const index = Number(await terminal.question(`${prompt.message}: `, { signal: prompt.signal })) - 1;
            const selected = prompt.options[index]; if (!selected) throw new Error('Invalid selection'); return selected.id;
          }
          return terminal.question(`${prompt.message}: `, { signal: prompt.signal });
        },
        notify: (event) => {
          if (event.type === 'auth_url') process.stdout.write(`${event.url}\n${event.instructions ?? ''}\n`);
          else if (event.type === 'device_code') process.stdout.write(`${event.verificationUri}\nCode: ${event.userCode}\n`);
          else process.stdout.write(`${event.message}\n`);
        },
      }, { getDeviceId: () => deviceId });
      process.stdout.write(`Saved ${values.provider} login in the v2 state directory.\n`);
    } finally { terminal.close(); await unlock(); }
    return;
  }
  if (action === 'init') {
    if (!values.provider || !values.model || !values.verify?.length) throw new Error('init requires --provider, --model, and explicit --verify commands');
    const root = await repositoryRoot(process.cwd());
    await writeFile(join(root, 'romp-v2.toml'), stringify({
      label: 'romp:v2', claimed_label: 'romp:v2:claimed', blocked_label: 'romp:v2:blocked', changes_requested_label: 'romp:v2:changes-requested',
      timeout_ms: 1_800_000, verify: { commands: values.verify }, agent: { provider: values.provider, model: values.model }, review: { enabled: true, max_fix_rounds: 2 },
    }), { flag: 'wx' });
    process.stdout.write('Created romp-v2.toml. Create its GitHub labels before running jobs.\n');
    return;
  }
  if (action === 'doctor') {
    const result = await preflight(process.cwd(), new GitHubCLI(process.cwd()), providerModels(stateDirectory()));
    print({ ...result, stateDirectory: stateDirectory(), provisioned: false }); return;
  }
  if (action === 'daemon') {
    if (id === 'start') {
      const controller = new AbortController();
      const stop = () => controller.abort();
      process.once('SIGINT', stop); process.once('SIGTERM', stop);
      try { await serve(stateDirectory(), { width: Number(values.width ?? 1), signal: controller.signal }); }
      finally { process.removeListener('SIGINT', stop); process.removeListener('SIGTERM', stop); }
      return;
    }
    if (id === 'stop') { print(await rpc('stop')); return; }
    if (id === 'status') { print(await rpc('ping')); return; }
    throw new Error('Expected daemon start, stop, or status');
  }
  if (action === 'run') {
    const job = await rpc('run', { root: process.cwd(), issue: Number(values.issue) }) as { id: string };
    print(job);
    if (!values.detach) {
      const result = await rpc('wait', { id: job.id }) as Job;
      print(result);
      if (result.outcome !== 'done') process.exitCode = 1;
    }
    return;
  }
  if (action === 'watch' || action === 'unwatch') { print(await rpc(action, { root: process.cwd() })); return; }
  if (action === 'status' || action === 'history') {
    const state = await rpc('state') as { jobs: Record<string, Job> };
    print(Object.values(state.jobs).filter((j) => action === 'history' ? Boolean(j.outcome) : !j.outcome)); return;
  }
  if (['job', 'wait', 'logs', 'cancel', 'steer'].includes(action)) {
    if (!id) throw new Error(`${action} requires a job ID`);
    const result = await rpc(action, { id, content: rest.join(' ') });
    print(result);
    if (action === 'wait' && (result as Job).outcome !== 'done') process.exitCode = 1;
    return;
  }
  throw new Error(`Unknown command ${action}`);
}
if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => { process.stderr.write(message(error) + '\n'); process.exitCode = 1; });
}
