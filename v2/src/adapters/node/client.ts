import { createConnection } from 'node:net';
import { randomUUID } from 'node:crypto';
import { join } from 'node:path';
import type { z } from 'zod';
import { FRAME_LIMIT, ResponseSchema, contracts } from '../../transport/protocol.js';
import { message } from '../../shared/errors.js';

function send(directory: string, method: string, params: unknown): Promise<unknown> {
  return new Promise((resolve, reject) => {
    const socket = createConnection(join(directory, 'romp.sock'));
    const id = randomUUID();
    let buffer = '';
    socket.setEncoding('utf8');
    socket.once('connect', () =>
      socket.write(JSON.stringify({ version: 2, id, method, params }) + '\n'),
    );
    socket.once('error', (error) =>
      reject(new Error(`Romp v2 daemon: ${message(error)}; start it with daemon start`)),
    );
    socket.on('data', (chunk: string) => {
      buffer += chunk;
      if (Buffer.byteLength(buffer) > FRAME_LIMIT) {
        socket.destroy();
        reject(new Error('Response exceeds frame limit'));
        return;
      }
      if (!buffer.includes('\n')) return;
      try {
        const value: unknown = JSON.parse(buffer);
        const response = ResponseSchema.parse(value);
        if (response.id !== id) throw new Error('Protocol response identity mismatch');
        if ('error' in response) throw new Error(response.error);
        resolve(response.result);
      } catch (error) {
        reject(error instanceof Error ? error : new Error(message(error), { cause: error }));
      } finally {
        socket.destroy();
      }
    });
    socket.once('end', () => {
      if (!buffer.includes('\n')) reject(new Error('Daemon closed before replying'));
    });
  });
}
function endpoint<P, R>(
  directory: string,
  method: string,
  contract: { params: z.ZodType<P>; result: z.ZodType<R> },
) {
  return async (params: P): Promise<R> =>
    contract.result.parse(await send(directory, method, contract.params.parse(params)));
}
export function createClient(directory: string) {
  return {
    ping: endpoint(directory, 'ping', contracts.ping),
    run: endpoint(directory, 'run', contracts.run),
    watch: endpoint(directory, 'watch', contracts.watch),
    unwatch: endpoint(directory, 'unwatch', contracts.unwatch),
    state: endpoint(directory, 'state', contracts.state),
    job: endpoint(directory, 'job', contracts.job),
    wait: endpoint(directory, 'wait', contracts.wait),
    logs: endpoint(directory, 'logs', contracts.logs),
    cancel: endpoint(directory, 'cancel', contracts.cancel),
    steer: endpoint(directory, 'steer', contracts.steer),
    stop: endpoint(directory, 'stop', contracts.stop),
  };
}
