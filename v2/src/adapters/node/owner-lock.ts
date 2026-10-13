import { hasErrorCode } from '../../shared/errors.js';
import { chmod, mkdir, open, readFile, unlink } from 'node:fs/promises';
import { join } from 'node:path';
import { removeDeadLock } from './ownership.js';

export async function acquireLock(directory: string): Promise<() => Promise<void>> {
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await chmod(directory, 0o700);
  const path = join(directory, 'owner.pid');
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const file = await open(path, 'wx', 0o600);
      try {
        await file.writeFile(String(process.pid));
        await file.sync();
      } finally {
        await file.close();
      }
      return async () => {
        await unlink(path);
      };
    } catch (error) {
      if (!hasErrorCode(error, 'EEXIST')) throw error;
      const pid = Number(await readFile(path, 'utf8'));
      if (!Number.isSafeInteger(pid) || pid < 1)
        throw new Error(`Invalid daemon lock at ${path}; inspect it before removing it`, {
          cause: error,
        });
      try {
        process.kill(pid, 0);
        throw new Error(`Romp v2 state is already owned by PID ${pid}`, { cause: error });
      } catch (probe) {
        if (!hasErrorCode(probe, 'ESRCH')) throw probe;
      }
      if (!removeDeadLock(path))
        throw new Error('Another process is reclaiming daemon ownership', { cause: error });
    }
  }
  throw new Error('Could not acquire daemon ownership');
}
