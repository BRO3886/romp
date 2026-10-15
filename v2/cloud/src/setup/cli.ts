import { Command, Option } from '@commander-js/extra-typings';
import { authenticate } from '../../../src/adapters/node/login.js';
import {
  concurrency,
  configureCommand,
  parseCommand,
  reportCommandError,
} from '../../../src/adapters/node/cli.js';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import {
  cloudLogin,
  cloudLogout,
  subscriptionDirectory,
  transferSubscription,
} from './subscription.js';
import { statusRequest } from './api.js';
import { runWrangler } from './deployment.js';
import { loadProfile, profilePath, saveProfile } from './profile.js';
import { doctor, savedProfile, setup, withProfileLock, type Dependencies } from './operations.js';

export async function main(args: string[], dependencies: Partial<Dependencies> = {}) {
  const d: Dependencies = {
    home: homedir(),
    cwd: process.cwd(),
    root: dirname(createRequire(import.meta.url).resolve('@romp/cloud/package.json')),
    env: process.env,
    fetch,
    runner: runWrangler,
    login: authenticate,
    output: (value) =>
      console.log(typeof value === 'string' ? value : JSON.stringify(value, null, 2)),
    ...dependencies,
  };
  const program = configureCommand(new Command())
    .name('romp cloud')
    .description('Set up and manage a Cloudflare deployment')
    .option('--profile <name>', 'Deployment profile', 'default')
    .configureHelp({ showGlobalOptions: true })
    .configureOutput({
      writeOut: (text) => d.output(text.trimEnd()),
      writeErr: (text) => process.stderr.write(text),
    });
  program
    .command('setup')
    .description('Deploy a Worker and connect the GitHub App webhook')
    .option('--account <id>', 'Cloudflare account ID (required on first setup)')
    .option('--name <worker>', 'Worker name (required on first setup)')
    .option('--app-id <id>', 'GitHub App ID (required on first setup)')
    .option('--private-key <file>', 'GitHub App PEM file (or GITHUB_PRIVATE_KEY)')
    .option(
      '--repo <owner/repo>',
      'Allowed repository (repeatable; defaults to Git origin)',
      collect,
    )
    .option('--config <file>', 'Romp config (defaults to repository-root romp-v2.toml)')
    .option('--dockerfile <file>', 'Runner image Dockerfile')
    .option('--concurrency <number>', 'Concurrent sandboxes, from 1 to 32', concurrency)
    .addOption(
      new Option('--openai-auth <mode>', 'OpenAI authentication mode').choices([
        'api-key',
        'subscription',
      ]),
    )
    .option('--dry-run', 'Validate access and build without provisioning or changing the webhook')
    .option('--replace-webhook', 'Replace a GitHub App webhook that points elsewhere')
    .addHelpText(
      'after',
      `
First setup requires Workers Paid/Containers, a Cloudflare API token, Docker,
and an installed GitHub App with Contents, Issues, and Pull requests write access.
Set CLOUDFLARE_API_TOKEN and create romp-v2.toml with romp init.
For ChatGPT, run romp cloud login first, then select --openai-auth subscription.

Example from your repository:
  romp cloud setup --account ACCOUNT_ID --name romp-cloud --app-id APP_ID \\
    --private-key /path/to/app.pem --openai-auth subscription --dry-run

Git origin supplies the repository when --repo is omitted on first setup.
Later runs reuse the selected profile, including its repository allowlist.
Review the dry run, then repeat without --dry-run to deploy.
`,
    )
    .action(async (options) => {
      await setup(program.opts().profile, options, d);
    });
  program
    .command('login')
    .description('Sign in to ChatGPT for this cloud deployment')
    .action(async () => {
      const name = program.opts().profile;
      const file = profilePath(d.home, name);
      const previous = await loadProfile(file);
      const directory = subscriptionDirectory(file);
      await withProfileLock(
        file,
        async () => {
          await cloudLogin(directory, d.login);
          if (previous?.openaiAuth === 'subscription' && previous.endpoint) {
            await transferSubscription(previous, directory, d.fetch);
            d.output(`Cloud subscription connected to ${previous.endpoint}. Profile: ${file}`);
          } else
            d.output(
              `Cloud login saved to ${join(directory, 'transfer.json')}.\nRun romp cloud setup --profile ${name} --openai-auth subscription to configure the deployment.`,
            );
        },
        previous,
      );
    });
  program
    .command('logout')
    .description('Revoke and remove the cloud subscription session')
    .action(async () => {
      const file = profilePath(d.home, program.opts().profile);
      const previous = await loadProfile(file);
      await withProfileLock(
        file,
        async () => {
          const result = await cloudLogout(previous, subscriptionDirectory(file), d.fetch);
          d.output(result);
          if (!result.remoteRevocation)
            d.output(
              'Remote revocation was not confirmed. Disconnect the app in ChatGPT Settings.',
            );
        },
        previous,
      );
    });
  program
    .command('doctor')
    .description('Check the deployed Worker, authentication, and webhook')
    .action(async () => {
      const { profile } = await savedProfile(program.opts().profile, d.home);
      await doctor(profile, d);
    });
  program
    .command('configure')
    .description('Set the maximum concurrent sandboxes')
    .requiredOption('--concurrency <number>', 'Concurrent sandboxes, from 1 to 32', concurrency)
    .action(async (options) => {
      const { file, profile } = await savedProfile(program.opts().profile, d.home);
      await withProfileLock(
        file,
        async () => {
          d.output(await statusRequest(profile, d.fetch, '/concurrency', options.concurrency));
          profile.concurrency = options.concurrency;
          await saveProfile(file, profile);
        },
        profile,
      );
    });
  for (const [action, description] of [
    ['status', 'Show queue, active runs, and authentication'],
    ['pause', 'Queue new work and let active runs finish'],
    ['resume', 'Start admitting queued work again'],
  ] as const) {
    program
      .command(action)
      .description(description)
      .action(async () => {
        const { profile } = await savedProfile(program.opts().profile, d.home);
        d.output(await statusRequest(profile, d.fetch, `/${action}`));
      });
  }
  await parseCommand(program, args.length ? args : ['--help']);
}
function collect(value: string, previous: string[] = []): string[] {
  return [...previous, value];
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  void main(process.argv.slice(2)).catch(reportCommandError);
}
