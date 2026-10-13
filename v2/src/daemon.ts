import { chmod, mkdir, open, readFile, unlink } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';
import { createServer, createConnection, type Socket } from 'node:net';
import { GitHubCLI } from './github.js';
import { message } from './domain.js';
import { Runtime } from './runtime.js';
import { removeDeadLock } from './ownership.js';

const frameLimit = 8 * 1024 * 1024;
export function stateDirectory(): string {
  return resolve(process.env.ROMP_V2_STATE ?? join(process.env.XDG_STATE_HOME ?? join(homedir(), '.local/state'), 'romp-v2'));
}
export async function acquireLock(directory: string): Promise<() => Promise<void>> {
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await chmod(directory, 0o700);
  const path = join(directory, 'owner.pid');
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const file = await open(path, 'wx', 0o600);
      try { await file.writeFile(String(process.pid)); await file.sync(); }
      finally { await file.close(); }
      return async () => { await unlink(path); };
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error;
      const pid = Number(await readFile(path, 'utf8'));
      if (!Number.isSafeInteger(pid) || pid < 1) throw new Error(`Invalid daemon lock at ${path}; inspect it before removing it`);
      try { process.kill(pid, 0); throw new Error(`Romp v2 state is already owned by PID ${pid}`); }
      catch (probe) {
        if ((probe as NodeJS.ErrnoException).code !== 'ESRCH') throw probe;
      }
      if (!removeDeadLock(path)) throw new Error('Another process is reclaiming daemon ownership');
    }
  }
  throw new Error('Could not acquire daemon ownership');
}
type Request = { version: 2; id: string; method: string; params: Record<string, unknown> };
function text(value: unknown, name: string) {
  if (typeof value !== 'string' || !value.trim()) throw new Error(`${name} must be a non-empty string`);
  return value;
}
export async function serve(directory: string, options: { width?: number; signal?: AbortSignal } = {}) {
  const unlock = await acquireLock(directory);
  let runtime: Runtime | undefined;
  const sockets = new Set<Socket>();
  const path = join(directory, 'romp.sock');
  const controller = new AbortController();
  const stop = () => controller.abort();
  options.signal?.addEventListener('abort', stop, { once: true });
  if (options.signal?.aborted) stop();
  const server = createServer((socket) => {
    sockets.add(socket);
    socket.once('close', () => sockets.delete(socket));
    socket.on('error', () => socket.destroy());
    let buffer = '';
    let accepted = false;
    socket.setEncoding('utf8');
    socket.setTimeout(30_000, () => socket.destroy());
    socket.on('data', (chunk: string) => {
      if (accepted) { socket.destroy(); return; }
      buffer += chunk;
      if (Buffer.byteLength(buffer) > frameLimit) { socket.destroy(); return; }
      if (!buffer.includes('\n')) return;
      accepted = true;
      socket.setTimeout(0);
      void (async () => {
        let id = '';
        try {
          const request: Request = JSON.parse(buffer);
          if (request.version !== 2 || typeof request.id !== 'string' || !request.params || typeof request.params !== 'object' || Array.isArray(request.params)) throw new Error('Invalid protocol v2 request');
          id = request.id;
          const p = request.params;
          let result: unknown;
          switch (request.method) {
            case 'ping': result = { version: '2.0.0-experimental.1' }; break;
            case 'run': result = await runtime!.enqueue(text(p.root, 'root'), Number(p.issue)); break;
            case 'watch': result = await runtime!.watch(text(p.root, 'root'), true); break;
            case 'unwatch': result = await runtime!.watch(text(p.root, 'root'), false); break;
            case 'state': await runtime!.reconcile(); result = await runtime!.state(); break;
            case 'job': result = await runtime!.job(text(p.id, 'id')); break;
            case 'wait': result = await runtime!.wait(text(p.id, 'id')); break;
            case 'logs': result = await runtime!.logs(text(p.id, 'id')); break;
            case 'cancel': await runtime!.cancel(text(p.id, 'id')); result = { cancelled: true }; break;
            case 'steer': result = await runtime!.steer(text(p.id, 'id'), text(p.content, 'content')); break;
            case 'stop': result = { suspended: true }; setTimeout(stop, 20); break;
            default: throw new Error(`Unknown method ${request.method}`);
          }
          const reply = JSON.stringify({ version: 2, id, result }) + '\n';
          if (Buffer.byteLength(reply) > frameLimit) throw new Error('Response exceeds protocol frame limit');
          socket.end(reply);
        } catch (error) { socket.end(JSON.stringify({ version: 2, id, error: message(error) }) + '\n'); }
      })();
    });
  });
  let polling: Promise<void> = Promise.resolve();
  let timer: NodeJS.Timeout | undefined;
  try {
    await unlink(path).catch((error: NodeJS.ErrnoException) => { if (error.code !== 'ENOENT') throw error; });
    runtime = await Runtime.open(directory, new GitHubCLI(process.cwd()), { width: options.width });
    await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(path, () => { server.removeListener('error', reject); resolve(); }); });
    await chmod(path, 0o600);
    process.stdout.write(`Romp v2 daemon listening on ${path}\n`);
    let running = false;
    const poll = () => {
      if (running) return;
      running = true;
      polling = runtime!.tick().catch((error) => process.stderr.write(`Scheduler: ${message(error)}\n`)).then(() => { running = false; });
    };
    poll(); timer = setInterval(poll, 5000);
    if (!controller.signal.aborted) await new Promise<void>((resolve) => controller.signal.addEventListener('abort', () => resolve(), { once: true }));
  } finally {
    if (timer) clearInterval(timer);
    await polling;
    await runtime?.close();
    for (const socket of sockets) socket.destroy();
    if (server.listening) await new Promise<void>((resolve) => server.close(() => resolve()));
    await unlink(path).catch((error: NodeJS.ErrnoException) => { if (error.code !== 'ENOENT') throw error; });
    await unlock();
    options.signal?.removeEventListener('abort', stop);
  }
}
export function rpc(method: string, params: Record<string, unknown> = {}, directory = stateDirectory()): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const socket = createConnection(join(directory, 'romp.sock'));
    const id = `${process.pid}-${Date.now()}`;
    socket.setEncoding('utf8');
    let buffer = '';
    socket.once('connect', () => socket.write(JSON.stringify({ version: 2, id, method, params }) + '\n'));
    socket.once('error', (error) => reject(new Error(`Romp v2 daemon: ${message(error)}; start it with daemon start`)));
    socket.on('data', (chunk: string) => {
      buffer += chunk;
      if (Buffer.byteLength(buffer) > frameLimit) { socket.destroy(); reject(new Error('Response exceeds frame limit')); return; }
      if (!buffer.includes('\n')) return;
      try {
        const response = JSON.parse(buffer);
        if (response.version !== 2 || response.id !== id) throw new Error('Protocol response identity mismatch');
        if (response.error) throw new Error(String(response.error));
        resolve(response.result);
      } catch (error) { reject(error); }
      finally { socket.destroy(); }
    });
    socket.once('end', () => { if (!buffer.includes('\n')) reject(new Error('Daemon closed before replying')); });
  });
}
