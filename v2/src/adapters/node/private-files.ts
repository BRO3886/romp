import { constants } from 'node:fs';
import { chmod, lstat, mkdir, open, rename, rm } from 'node:fs/promises';
import { dirname } from 'node:path';
import { randomUUID } from 'node:crypto';
import { hasErrorCode } from '../../shared/errors.js';

export async function readPrivateJson(file: string): Promise<unknown> {
  try {
    const handle = await open(file, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const stat = await handle.stat();
      if (!stat.isFile() || stat.mode & 0o077)
        throw new Error('Credential metadata must be a private file (chmod 600)');
      if (stat.size > 1024 * 1024) throw new Error('Credential metadata exceeds 1 MiB');
      const text = await handle.readFile('utf8');
      try {
        return JSON.parse(text) as unknown;
      } catch (error) {
        throw new Error(`Invalid credential JSON at ${file}`, { cause: error });
      }
    } finally {
      await handle.close();
    }
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return undefined;
    throw error;
  }
}
export async function privateDirectory(path: string) {
  await mkdir(path, { recursive: true, mode: 0o700 });
  if (!(await lstat(path)).isDirectory())
    throw new Error('Credential directory must not be a symlink');
  await chmod(path, 0o700);
}
export async function savePrivateJson(file: string, value: unknown) {
  await privateDirectory(dirname(file));
  const temporary = `${file}.${randomUUID()}.tmp`;
  try {
    const handle = await open(temporary, 'wx', 0o600);
    try {
      await handle.writeFile(`${JSON.stringify(value)}\n`);
      await handle.sync();
    } finally {
      await handle.close();
    }
    await rename(temporary, file);
  } finally {
    await rm(temporary, { force: true });
  }
}
