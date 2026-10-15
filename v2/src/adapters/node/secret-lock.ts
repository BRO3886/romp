import { open, readFile, unlink } from 'node:fs/promises';
import { basename, dirname, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { hasErrorCode } from '../../shared/errors.js';
import { removeDeadLock } from './ownership.js';
import { privateDirectory } from './private-files.js';

export async function withSecretLock<T>(
  file: string,
  action: () => Promise<T>,
  signal?: AbortSignal,
): Promise<T> {
  signal?.throwIfAborted();
  await privateDirectory(dirname(file));
  const path =
    basename(file) === 'auth.json' ? join(dirname(file), 'auth.lock') : `${file}.secret.lock`;
  for (let attempt = 0; attempt < 300; attempt++) {
    signal?.throwIfAborted();
    let handle;
    try {
      handle = await open(path, 'wx', 0o600);
    } catch (error) {
      if (!hasErrorCode(error, 'EEXIST')) throw error;
      let pid: number;
      try {
        pid = Number(await readFile(path, 'utf8'));
      } catch (readError) {
        if (!hasErrorCode(readError, 'ENOENT')) throw readError;
        continue;
      }
      if (Number.isSafeInteger(pid) && pid > 0 && removeDeadLock(path)) continue;
      await delay(100, undefined, { signal });
      continue;
    }
    try {
      try {
        await handle.writeFile(String(process.pid));
        await handle.sync();
      } finally {
        await handle.close();
      }
      return await action();
    } finally {
      await unlink(path);
    }
  }
  throw new Error('Credential store is busy');
}
