import type { JobInput, Verification } from '../../src/domain/model.js';
import type { Workspace } from '../../src/application/ports.js';

export type CommandResult = { stdout: string; stderr: string; exitCode: number };
export interface Sandbox {
  exec(
    args: string[],
    options?: { cwd?: string; env?: Record<string, string>; signal?: AbortSignal },
  ): Promise<CommandResult>;
  checkpoint(): Promise<void>;
  destroy(): Promise<void>;
}
async function readStream(stream: ReadableStream | null, limit: number) {
  if (!stream) return '';
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let output = '';
  let bytes = 0;
  try {
    for (;;) {
      const chunk: ReadableStreamReadResult<unknown> = await reader.read();
      if (chunk.done) break;
      const value = chunk.value;
      if (!(value instanceof Uint8Array)) throw new Error('Invalid command stream');
      bytes += value.length;
      if (bytes > limit) throw new Error('Command output exceeds 4 MiB');
      output += decoder.decode(value, { stream: true });
    }
    return output + decoder.decode();
  } finally {
    reader.releaseLock();
  }
}
export class CloudSandbox implements Sandbox {
  private initialized = false;
  constructor(private readonly ctx: DurableObjectState) {}
  private get container() {
    if (!this.ctx.container) throw new Error('Container binding missing');
    return this.ctx.container;
  }
  private async ensure() {
    if (this.initialized) return;
    const container = this.container;
    // Restart from the last published filesystem receipt before replaying durable work.
    if (container.running) await container.destroy();
    const snapshot = await this.ctx.storage.get<string>('workspace-snapshot');
    const image = container.images['runner'];
    if (!image) throw new Error('Runner image missing');
    container.start({
      ...(snapshot ? { containerSnapshot: { id: snapshot } } : { image }),
      enableInternet: true,
      entrypoint: ['sleep', 'infinity'],
    });
    await container.setInactivityTimeout(120_000);
    this.initialized = true;
  }
  async exec(
    args: string[],
    options: { cwd?: string; env?: Record<string, string>; signal?: AbortSignal } = {},
  ) {
    await this.ensure();
    await this.container.setInactivityTimeout(120_000);
    const abort = () => {
      this.initialized = false;
      this.ctx.waitUntil(this.container.destroy());
    };
    options.signal?.throwIfAborted();
    options.signal?.addEventListener('abort', abort, { once: true });
    try {
      const process = await this.container.exec(
        ['timeout', '--kill-after=2s', '90s', ...args],
        options,
      );
      const [stdout, stderr, exitCode] = await Promise.all([
        readStream(process.stdout, 4 * 1024 * 1024),
        readStream(process.stderr, 4 * 1024 * 1024),
        process.exitCode,
      ]);
      return { stdout, stderr, exitCode };
    } catch (error) {
      await this.destroy();
      throw error;
    } finally {
      options.signal?.removeEventListener('abort', abort);
    }
  }
  async checkpoint() {
    const snapshot = await this.container.snapshotContainer({ name: 'romp-workspace' });
    await this.ctx.storage.put('workspace-snapshot', snapshot.id);
  }
  async destroy() {
    await this.container.destroy();
    this.initialized = false;
  }
}
export class SandboxWorkspace implements Workspace {
  constructor(
    private readonly sandbox: Sandbox,
    private readonly token: () => Promise<string>,
  ) {}
  async command(args: string[], cwd = '/workspace', signal?: AbortSignal, authenticated = false) {
    const result = await this.sandbox.exec(args, {
      cwd,
      ...(signal ? { signal } : {}),
      ...(authenticated
        ? {
            env: {
              GIT_CONFIG_COUNT: '1',
              GIT_CONFIG_KEY_0: 'http.https://github.com/.extraheader',
              GIT_CONFIG_VALUE_0: `AUTHORIZATION: basic ${btoa(`x-access-token:${await this.token()}`)}`,
              GIT_TERMINAL_PROMPT: '0',
            },
          }
        : {}),
    });
    if (result.exitCode !== 0)
      throw new Error(
        `Command ${args[0]} failed (${result.exitCode}): ${result.stderr.slice(-4096)}`,
      );
    return result.stdout;
  }
  private git(args: string[], cwd: string, signal?: AbortSignal, authenticated = false) {
    return this.command(
      ['git', '-c', 'core.hooksPath=/dev/null', ...args],
      cwd,
      signal,
      authenticated,
    );
  }
  async base(root: string, base: string) {
    await this.git(['check-ref-format', '--branch', base], root);
    await this.git(['fetch', 'origin', base], root, undefined, true);
    return (await this.git(['rev-parse', 'FETCH_HEAD'], root)).trim();
  }
  async prepare(job: JobInput, signal: AbortSignal) {
    const result = await this.sandbox.exec(['bash', '-lc', 'test -d /workspace/job']);
    if (result.exitCode === 0) {
      if (
        (await this.git(['rev-parse', '--show-toplevel'], job.worktree, signal)).trim() !==
          job.worktree ||
        (await this.git(['branch', '--show-current'], job.worktree, signal)).trim() !== job.branch
      )
        throw new Error('Workspace does not match run');
      await this.git(['merge-base', '--is-ancestor', job.baseSha, 'HEAD'], job.worktree, signal);
    } else
      await this.git(
        ['worktree', 'add', '-b', job.branch, job.worktree, job.baseSha],
        job.root,
        signal,
      );
    await this.sandbox.checkpoint();
  }
  async commit(job: JobInput, subject: string, signal: AbortSignal) {
    await this.git(['add', '--all'], job.worktree, signal);
    const changed = await this.sandbox.exec(['git', 'diff', '--cached', '--quiet'], {
      cwd: job.worktree,
      signal,
    });
    if (changed.exitCode === 1) await this.git(['commit', '-m', subject], job.worktree, signal);
    else if (changed.exitCode !== 0) throw new Error('Cannot inspect staged diff');
    await this.sandbox.checkpoint();
    return this.head(job, signal);
  }
  async head(job: JobInput, signal: AbortSignal) {
    return (await this.git(['rev-parse', 'HEAD'], job.worktree, signal)).trim();
  }
  async diff(job: JobInput, signal: AbortSignal) {
    return this.git(['diff', `${job.baseSha}..HEAD`, '--'], job.worktree, signal);
  }
  async clean(job: JobInput, signal: AbortSignal) {
    if ((await this.git(['status', '--porcelain'], job.worktree, signal)).trim())
      throw new Error('Verification mutated workspace');
  }
  async verify(job: JobInput, signal: AbortSignal): Promise<Verification[]> {
    const results: Verification[] = [];
    for (const command of job.config.verify) {
      const result = await this.sandbox.exec(['bash', '-lc', command], {
        cwd: job.worktree,
        signal,
      });
      results.push({ command, output: result.stdout + result.stderr, exitCode: result.exitCode });
      if (result.exitCode !== 0) break;
    }
    await this.sandbox.checkpoint();
    return results;
  }
  async push(job: JobInput, sha: string, signal: AbortSignal) {
    await this.clean(job, signal);
    if ((await this.head(job, signal)) !== sha) throw new Error('HEAD changed after verification');
    await this.git(['push', 'origin', `HEAD:refs/heads/${job.branch}`], job.worktree, signal, true);
  }
  async cleanup(_job: JobInput, _signal?: AbortSignal) {
    await this.sandbox.checkpoint();
  }
  async readArtifact(job: JobInput, name: 'blocked.md' | 'pull-request.md') {
    const result = await this.sandbox.exec([
      'node',
      '-e',
      'const fs=require("fs"); try{process.stdout.write(fs.readFileSync(process.argv[1],"utf8"))}catch(e){if(e.code!=="ENOENT")throw e}',
      `${job.worktree}/.romp/${name}`,
    ]);
    if (result.exitCode) throw new Error('Cannot read artifact');
    return result.stdout;
  }
  async removeArtifact(job: JobInput, name: 'pull-request.md') {
    await this.command(['rm', '-f', `${job.worktree}/.romp/${name}`]);
    await this.sandbox.checkpoint();
  }
}
