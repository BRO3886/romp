import { createServer } from 'node:http';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { z } from 'zod';
import type { TestProject } from 'vitest/node';
const exec = promisify(execFile);
const command = async (args: string[]) => {
  try {
    const value = await exec('docker', args, { maxBuffer: 4 * 1024 * 1024, timeout: 100_000 });
    return { stdout: value.stdout, stderr: value.stderr, exitCode: 0 };
  } catch (error) {
    const result = z
      .object({ stdout: z.string(), stderr: z.string(), code: z.number() })
      .parse(error);
    return { stdout: result.stdout, stderr: result.stderr, exitCode: result.code };
  }
};
export default async function setup(project: TestProject) {
  const directory = await mkdtemp(join(tmpdir(), 'romp-cloud-docker-'));
  const image = `romp-cloud-test:${Date.now()}`;
  await exec('docker', ['build', '--quiet', '-t', image, '.'], { maxBuffer: 4 * 1024 * 1024 });
  const instances = new Map<string, { name: string; snapshot?: string; running: boolean }>();
  const prefix = `romp-test-${Date.now()}`;
  async function start(id: string) {
    let entry = instances.get(id);
    if (!entry) {
      entry = { name: `${prefix}-${id}`, running: false };
      instances.set(id, entry);
    }
    if (!entry.running) {
      await exec('docker', ['run', '-d', '--name', entry.name, image]);
      entry.running = true;
      if (entry.snapshot) {
        await exec('docker', ['cp', `${entry.snapshot}/.`, `${entry.name}:/workspace`]);
        await exec('docker', ['exec', entry.name, 'chown', '-R', '0:0', '/workspace']);
      } else {
        const script =
          'git init --bare /workspace/remote.git && git init -b main /workspace/repo && cd /workspace/repo && printf 0 > answer.txt && git add . && git commit -m "test: initial" && git remote add origin /workspace/remote.git && git push origin main';
        const seeded = await command(['exec', entry.name, 'bash', '-lc', script]);
        if (seeded.exitCode) throw new Error(seeded.stderr);
      }
    }
    return entry;
  }
  const server = createServer((req, res) => {
    void (async () => {
      const match = /^\/(r\d+-i\d+)\/(exec|checkpoint|destroy)$/.exec(req.url ?? '');
      if (!match?.[1] || !match[2]) {
        res.writeHead(404).end();
        return;
      }
      const id = match[1];
      const action = match[2];
      if (action === 'destroy') {
        const entry = instances.get(id);
        if (entry?.running) {
          await exec('docker', ['rm', '-f', entry.name]);
          entry.running = false;
        }
        res.end('{}');
        return;
      }
      const entry = await start(id);
      if (action === 'checkpoint') {
        const snapshot = await mkdtemp(join(directory, 'snapshot-'));
        await exec('docker', ['cp', `${entry.name}:/workspace/.`, snapshot]);
        entry.snapshot = snapshot;
        res.end('{}');
        return;
      }
      const chunks: Uint8Array[] = [];
      for await (const chunk of req) {
        const value: unknown = chunk;
        if (!(value instanceof Uint8Array)) throw new Error('Bad body');
        chunks.push(value);
      }
      const bytes = new Uint8Array(chunks.reduce((size, chunk) => size + chunk.length, 0));
      let offset = 0;
      for (const chunk of chunks) {
        bytes.set(chunk, offset);
        offset += chunk.length;
      }
      const input = z
        .object({ args: z.array(z.string()), cwd: z.string().optional() })
        .parse(JSON.parse(new TextDecoder().decode(bytes)) as unknown);
      const result = await command([
        'exec',
        '-w',
        input.cwd ?? '/workspace',
        entry.name,
        'timeout',
        '--kill-after=2s',
        '90s',
        ...input.args,
      ]);
      res.setHeader('content-type', 'application/json');
      res.end(JSON.stringify(result));
    })().catch((error: unknown) => {
      res.writeHead(500).end(error instanceof Error ? error.message : 'Server failed');
    });
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Server address missing');
  project.provide('runnerUrl', `http://127.0.0.1:${address.port}`);
  return async () => {
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve())),
    );
    for (const entry of instances.values())
      if (entry.running) await exec('docker', ['rm', '-f', entry.name]);
    await exec('docker', ['image', 'rm', image]);
    await rm(directory, { recursive: true, force: true });
  };
}
