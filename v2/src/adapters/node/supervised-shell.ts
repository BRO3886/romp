import { spawn } from 'node:child_process';
import { mkdirSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const [ownerText, directory, token, script] = process.argv.slice(2);
const owner = Number(ownerText);
if (!directory || !token || script === undefined || !Number.isSafeInteger(owner) || owner < 1)
  throw new Error('Invalid shell supervision arguments');
mkdirSync(directory, { recursive: true, mode: 0o700 });
const path = join(directory, `${token}.json`);
writeFileSync(path, JSON.stringify({ pid: process.pid, owner, token }), {
  mode: 0o600,
  flag: 'wx',
});
const stop = () => {
  try {
    process.kill(-process.pid, 'SIGKILL');
  } catch {
    process.exit(1);
  }
};
try {
  process.kill(owner, 0);
} catch {
  stop();
}
const timer = setInterval(() => {
  try {
    process.kill(owner, 0);
  } catch {
    stop();
  }
}, 100);
const child = spawn('/bin/bash', ['-c', script], { stdio: 'inherit' });
child.once('error', (error) => {
  process.stderr.write(`${error.message}\n`);
  clearInterval(timer);
  unlinkSync(path);
  process.exitCode = 1;
});
child.once('exit', (code, signal) => {
  clearInterval(timer);
  unlinkSync(path);
  process.exitCode = signal ? 1 : (code ?? 1);
});
