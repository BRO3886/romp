import {
  CommanderError,
  InvalidArgumentError,
  type CommandUnknownOpts,
} from '@commander-js/extra-typings';

export function positiveInteger(value: string): number {
  if (!/^[1-9]\d*$/.test(value) || !Number.isSafeInteger(Number(value)))
    throw new InvalidArgumentError('must be a positive integer');
  return Number(value);
}

export function concurrency(value: string): number {
  if (!/^[1-9]\d*$/.test(value) || Number(value) > 32)
    throw new InvalidArgumentError('must be an integer from 1 to 32');
  return Number(value);
}

export function configureCommand<T extends CommandUnknownOpts>(program: T): T {
  program.exitOverride((error) => {
    if (error.code === 'commander.executeSubCommandAsync') process.exitCode = error.exitCode;
    else throw error;
  });
  program.showHelpAfterError();
  return program;
}

export async function parseCommand(program: CommandUnknownOpts, args: string[]) {
  try {
    await program.parseAsync(args, { from: 'user' });
  } catch (error) {
    if (!(error instanceof CommanderError && error.exitCode === 0)) throw error;
  }
}

export function reportCommandError(error: unknown) {
  if (!(error instanceof CommanderError))
    process.stderr.write(`Error: ${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
}
