import type { Command } from '@commander-js/extra-typings';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { stringify } from 'smol-toml';
import { concurrency, positiveInteger } from '../adapters/node/cli.js';
import { createClient } from '../adapters/node/client.js';
import { GitHubCLI } from '../adapters/node/github.js';
import { authenticate } from '../adapters/node/login.js';
import { providerModels } from '../adapters/node/models.js';
import { stateDirectory } from '../adapters/node/paths.js';
import { LocalRepositories } from '../adapters/node/repositories.js';

const print = (data: unknown) => process.stdout.write(JSON.stringify(data, null, 2) + '\n');
const client = () => createClient(stateDirectory());
export function registerCommands(program: Command) {
  program
    .command('init')
    .description('Create configuration in the current repository')
    .requiredOption('--provider <name>', 'Model provider')
    .requiredOption('--model <id>', 'Model ID')
    .requiredOption('--verify <command>', 'Verification command (repeatable)', collect)
    .action(async (options) => {
      const root = await new LocalRepositories().root(process.cwd());
      await writeFile(
        join(root, 'romp-v2.toml'),
        stringify({
          label: 'romp:v2',
          claimed_label: 'romp:v2:claimed',
          blocked_label: 'romp:v2:blocked',
          changes_requested_label: 'romp:v2:changes-requested',
          timeout_ms: 1_800_000,
          verify: { commands: options.verify },
          agent: { provider: options.provider, model: options.model },
          review: { enabled: true, max_fix_rounds: 2 },
        }),
        { flag: 'wx' },
      );
      process.stdout.write('Created romp-v2.toml. Create its GitHub labels before running jobs.\n');
    });
  program
    .command('models')
    .description('List supported models')
    .option('--provider <name>', 'Filter by provider')
    .action((options) => {
      print(
        providerModels()
          .getModels(options.provider)
          .map((m) => ({ provider: m.provider, model: m.id })),
      );
    });
  for (const action of ['login', 'logout'] as const) {
    program
      .command(action)
      .description(`${action === 'login' ? 'Sign in to' : 'Sign out of'} a local model provider`)
      .requiredOption('--provider <name>', 'Model provider')
      .action(async (options) => {
        await authenticate(action, options.provider, stateDirectory());
      });
  }
  program
    .command('doctor')
    .description('Check local configuration and authentication')
    .action(async () => {
      const { preflight } = await import('../composition/node.js');
      const result = await preflight(
        process.cwd(),
        new GitHubCLI(process.cwd()),
        providerModels(stateDirectory()),
      );
      print({ ...result, stateDirectory: stateDirectory(), provisioned: false });
    });
  const daemon = program.command('daemon').description('Manage the local durable daemon');
  daemon
    .command('start')
    .description('Run the daemon in this terminal')
    .option('--width <number>', 'Concurrent jobs, from 1 to 32', concurrency, 1)
    .action(async (options) => {
      const { serve } = await import('../adapters/node/daemon.js');
      const controller = new AbortController();
      const stop = () => controller.abort();
      process.once('SIGINT', stop);
      process.once('SIGTERM', stop);
      try {
        await serve(stateDirectory(), { width: options.width, signal: controller.signal });
      } finally {
        process.removeListener('SIGINT', stop);
        process.removeListener('SIGTERM', stop);
      }
    });
  daemon
    .command('stop')
    .description('Suspend jobs and stop the daemon')
    .action(async () => {
      print(await client().stop({}));
    });
  daemon
    .command('status')
    .description('Check the running daemon')
    .action(async () => {
      print(await client().ping({}));
    });
  program
    .command('run')
    .description('Run one GitHub issue')
    .requiredOption('-i, --issue <number>', 'Issue number', positiveInteger)
    .option('--detach', 'Return without waiting for completion')
    .action(async (options) => {
      const connection = client();
      const job = await connection.run({ root: process.cwd(), issue: options.issue });
      print(job);
      if (!options.detach) {
        const result = await connection.wait({ id: job.id });
        print(result);
        if (result.outcome !== 'done') process.exitCode = 1;
      }
    });
  for (const action of ['watch', 'unwatch'] as const) {
    program
      .command(action)
      .description(
        action === 'watch'
          ? 'Watch the current repository'
          : 'Stop admitting issues from this repository',
      )
      .action(async () => {
        print(await client()[action]({ root: process.cwd() }));
      });
  }
  for (const action of ['status', 'history'] as const) {
    program
      .command(action)
      .description(action === 'status' ? 'List unfinished jobs' : 'List finished jobs')
      .action(async () => {
        const state = await client().state({});
        print(
          Object.values(state.jobs).filter((job) =>
            action === 'history' ? Boolean(job.outcome) : !job.outcome,
          ),
        );
      });
  }
  for (const [action, description] of [
    ['job', 'Show a job'],
    ['logs', 'Show job events'],
    ['cancel', 'Cancel a job'],
  ] as const) {
    program
      .command(action)
      .description(description)
      .argument('<id>', 'Job ID')
      .action(async (id) => {
        print(await client()[action]({ id }));
      });
  }
  program
    .command('wait')
    .description('Wait for a job to finish')
    .argument('<id>', 'Job ID')
    .action(async (id) => {
      const result = await client().wait({ id });
      print(result);
      if (result.outcome !== 'done') process.exitCode = 1;
    });
  program
    .command('steer')
    .description('Send guidance to a job')
    .argument('<id>', 'Job ID')
    .argument('<message...>', 'Guidance text')
    .action(async (id, words) => {
      print(await client().steer({ id, content: words.join(' ') }));
    });
}
function collect(value: string, previous: string[] = []): string[] {
  return [...previous, value];
}
