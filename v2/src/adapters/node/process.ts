import { hasErrorCode } from '../../shared/errors.js';
import { spawn } from 'node:child_process';

export type CommandResult = { stdout: string; stderr: string; code: number };
export class CommandError extends Error {
  constructor(
    public readonly result: CommandResult,
    command: string,
  ) {
    super(`${command} exited ${result.code}\n${result.stderr || result.stdout}`);
  }
}
export async function command(
  executable: string,
  args: string[],
  cwd: string,
  signal?: AbortSignal,
  options: { allowFailure?: boolean; maxBytes?: number; env?: NodeJS.ProcessEnv } = {},
): Promise<CommandResult> {
  signal?.throwIfAborted();
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, {
      cwd,
      env: options.env,
      stdio: ['ignore', 'pipe', 'pipe'],
      detached: process.platform !== 'win32',
    });
    let stdout = '';
    let stderr = '';
    const limit = options.maxBytes ?? 4 * 1024 * 1024;
    let overflow = false;
    const kill = () => {
      if (!child.pid) return;
      try {
        process.kill(process.platform === 'win32' ? child.pid : -child.pid, 'SIGKILL');
      } catch (error) {
        if (!hasErrorCode(error, 'ESRCH')) child.kill('SIGKILL');
      }
    };
    signal?.addEventListener('abort', kill, { once: true });
    child.stdout.on('data', (chunk: Buffer) => {
      stdout += chunk.toString();
      if (Buffer.byteLength(stdout) + Buffer.byteLength(stderr) > limit) {
        overflow = true;
        kill();
      }
    });
    child.stderr.on('data', (chunk: Buffer) => {
      stderr += chunk.toString();
      if (Buffer.byteLength(stdout) + Buffer.byteLength(stderr) > limit) {
        overflow = true;
        kill();
      }
    });
    child.once('error', (error) => {
      signal?.removeEventListener('abort', kill);
      reject(error);
    });
    child.once('close', (code) => {
      signal?.removeEventListener('abort', kill);
      if (signal?.aborted) {
        reject(
          signal.reason instanceof Error
            ? signal.reason
            : new Error('Command cancelled', { cause: signal.reason }),
        );
        return;
      }
      if (overflow) {
        reject(new Error(`${executable} output exceeded ${limit} bytes`));
        return;
      }
      const result = { stdout, stderr, code: code ?? 1 };
      if (result.code !== 0 && !options.allowFailure) reject(new CommandError(result, executable));
      else resolve(result);
    });
    if (signal?.aborted) kill();
  });
}
