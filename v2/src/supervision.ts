import { randomUUID } from 'node:crypto';
import { readdir, readFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
import { command } from './process.js';

export function supervisedArguments(directory: string, script: string): string[] {
  const source = fileURLToPath(new URL(import.meta.url.endsWith('.ts') ? './supervised-shell.ts' : './supervised-shell.js', import.meta.url));
  return [...(source.endsWith('.ts') ? ['--import', import.meta.resolve('tsx')] : []), source, String(process.pid), directory, randomUUID(), script];
}
export function shellQuote(value: string): string { return `'${value.replaceAll("'", "'\\''")}'`; }
export async function recoverShells(directory: string) {
  let files: string[];
  try { files = await readdir(directory); }
  catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return; throw error; }
  for (const file of files.filter((f) => f.endsWith('.json'))) {
    const path = join(directory, file);
    let record: { pid: number; token: string };
    try { record = JSON.parse(await readFile(path, 'utf8')); }
    catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') continue; throw error; }
    if (!Number.isSafeInteger(record.pid) || record.pid < 1 || typeof record.token !== 'string') throw new Error(`Invalid process receipt ${path}`);
    const processInfo = await command('ps', ['-p', String(record.pid), '-o', 'args='], directory, undefined, { allowFailure: true });
    if (processInfo.code === 0 && /supervised-shell\.(js|ts)/.test(processInfo.stdout) && processInfo.stdout.includes(record.token)) {
      try { process.kill(-record.pid, 'SIGKILL'); }
      catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ESRCH') throw error; }
      for (let i = 0; i < 100; i++) {
        try { process.kill(-record.pid, 0); }
        catch (error) { if ((error as NodeJS.ErrnoException).code === 'ESRCH') break; throw error; }
        if (i === 99) throw new Error(`Previous shell group ${record.pid} has not stopped`);
        await delay(25);
      }
    }
    await rm(path);
  }
}
