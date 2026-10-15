#!/usr/bin/env node
import { Command } from '@commander-js/extra-typings';
import { realpathSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { registerCommands } from './commands/local.js';
import { configureCommand, parseCommand, reportCommandError } from './adapters/node/cli.js';
import { VERSION } from './transport/protocol.js';

export async function main(args: string[]) {
  const program = configureCommand(new Command())
    .name('romp')
    .description('Romp v2 (experimental, independent of v1)')
    .version(VERSION)
    .addHelpText(
      'after',
      '\nConfig: romp-v2.toml. ROMP_V2_STATE overrides the local state directory.',
    );
  registerCommands(program);
  program.command('cloud', 'Set up and manage a Cloudflare deployment', {
    executableFile: fileURLToPath(new URL('../cloud/dist/cloud/src/setup/cli.js', import.meta.url)),
  });
  await parseCommand(program, args.length ? args : ['--help']);
}

if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  void main(process.argv.slice(2)).catch(reportCommandError);
}
