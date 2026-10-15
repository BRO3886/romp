import { resolve } from 'node:path';
import { command } from '../../../src/adapters/node/process.js';

export async function repositoryRoot(cwd: string): Promise<string> {
  const result = await command(
    'git',
    ['rev-parse', '--show-toplevel'],
    cwd,
    AbortSignal.timeout(10_000),
    { allowFailure: true },
  );
  return result.code === 0 ? resolve(result.stdout.trim()) : cwd;
}
export async function originRepository(root: string): Promise<string> {
  const result = await command(
    'git',
    ['remote', 'get-url', 'origin'],
    root,
    AbortSignal.timeout(10_000),
    { allowFailure: true },
  );
  const match =
    /^(?:https:\/\/github\.com\/|git@github\.com:|ssh:\/\/git@github\.com\/)([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+?)(?:\.git)?\/?$/i.exec(
      result.stdout.trim(),
    );
  if (result.code !== 0 || !match)
    throw new Error(
      'Cannot detect a GitHub.com repository from Git origin. Run setup in your repository or pass --repo OWNER/REPO.',
    );
  return `${match[1]}/${match[2]}`;
}
