import { chmod, unlink } from 'node:fs/promises';
import { join } from 'node:path';
import { createServer, type Socket } from 'node:net';
import { GitHubCLI } from './github.js';
import { message } from '../../shared/errors.js';
import { openLocalRuntime } from '../../composition/node.js';
import type { JobService } from '../../application/job-service.js';
import { createHandler } from '../../transport/handler.js';
import { FRAME_LIMIT, parseRequest } from '../../transport/protocol.js';
import { acquireLock } from './owner-lock.js';

export async function serve(
  directory: string,
  options: { width?: number; signal?: AbortSignal } = {},
) {
  const unlock = await acquireLock(directory);
  let runtime: JobService | undefined;
  let handle: ReturnType<typeof createHandler> | undefined;
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
      if (accepted) {
        socket.destroy();
        return;
      }
      buffer += chunk;
      if (Buffer.byteLength(buffer) > FRAME_LIMIT) {
        socket.destroy();
        return;
      }
      if (!buffer.includes('\n')) return;
      accepted = true;
      socket.setTimeout(0);
      void (async () => {
        let id = '';
        try {
          const request = parseRequest(buffer);
          id = request.id;
          if (!handle) throw new Error('Daemon is not ready');
          const result = await handle(request);
          const reply = JSON.stringify({ version: 2, id, result }) + '\n';
          if (Buffer.byteLength(reply) > FRAME_LIMIT)
            throw new Error('Response exceeds protocol frame limit');
          socket.end(reply);
        } catch (error) {
          socket.end(JSON.stringify({ version: 2, id, error: message(error) }) + '\n');
        }
      })();
    });
  });
  let polling: Promise<void> = Promise.resolve();
  let timer: NodeJS.Timeout | undefined;
  try {
    await unlink(path).catch((error: NodeJS.ErrnoException) => {
      if (error.code !== 'ENOENT') throw error;
    });
    runtime = await openLocalRuntime(
      directory,
      new GitHubCLI(process.cwd()),
      options.width === undefined ? {} : { width: options.width },
    );
    handle = createHandler(runtime, () => {
      setTimeout(stop, 20);
    });
    await new Promise<void>((resolve, reject) => {
      server.once('error', reject);
      server.listen(path, () => {
        server.removeListener('error', reject);
        resolve();
      });
    });
    await chmod(path, 0o600);
    process.stdout.write(`Romp v2 daemon listening on ${path}\n`);
    let running = false;
    const poll = () => {
      if (running) return;
      running = true;
      const active = runtime;
      if (!active) return;
      polling = active
        .tick()
        .catch((error) => process.stderr.write(`Scheduler: ${message(error)}\n`))
        .then(() => {
          running = false;
        });
    };
    poll();
    timer = setInterval(poll, 5000);
    if (!controller.signal.aborted)
      await new Promise<void>((resolve) =>
        controller.signal.addEventListener('abort', () => resolve(), { once: true }),
      );
  } finally {
    if (timer) clearInterval(timer);
    await polling;
    await runtime?.close();
    for (const socket of sockets) socket.destroy();
    if (server.listening) await new Promise<void>((resolve) => server.close(() => resolve()));
    await unlink(path).catch((error: NodeJS.ErrnoException) => {
      if (error.code !== 'ENOENT') throw error;
    });
    await unlock();
    options.signal?.removeEventListener('abort', stop);
  }
}
