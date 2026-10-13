import { mkdirSync, readFileSync, rmdirSync, unlinkSync } from 'node:fs';

export function removeDeadLock(path: string): boolean {
  const gate = `${path}.reclaim`;
  try { mkdirSync(gate, { mode: 0o700 }); }
  catch (error) { if ((error as NodeJS.ErrnoException).code === 'EEXIST') return false; throw error; }
  try {
    let pid: number;
    try { pid = Number(readFileSync(path, 'utf8')); }
    catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return true; throw error; }
    if (!Number.isSafeInteger(pid) || pid < 1) return false;
    try { process.kill(pid, 0); return false; }
    catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ESRCH') throw error; }
    unlinkSync(path);
    return true;
  } finally { rmdirSync(gate); }
}
