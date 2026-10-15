import type { Workspace } from '../../application/ports.js';
import { hasErrorCode } from '../../shared/errors.js';
import { randomUUID } from 'node:crypto';
import { mkdir, stat, readFile, rm } from 'node:fs/promises';
import { dirname, join, resolve } from 'node:path';
import { command } from './process.js';
import type { JobInput, Verification } from '../../domain/model.js';
import { supervisedArguments } from './supervision.js';

export class Git implements Workspace {
  async base(root: string, base: string): Promise<string> {
    await command('git', ['check-ref-format', '--branch', base], root);
    const ref = `refs/romp/fetch/${randomUUID()}`;
    const source = base.startsWith('refs/heads/') ? base : `refs/heads/${base}`;
    try {
      await command(
        'git',
        ['fetch', '--no-write-fetch-head', '--no-tags', '--refmap=', 'origin', `${source}:${ref}`],
        root,
      );
      return (
        await command('git', ['rev-parse', '--verify', `${ref}^{commit}`], root)
      ).stdout.trim();
    } finally {
      await command('git', ['update-ref', '-d', ref], root);
    }
  }
  async prepare(job: JobInput, signal: AbortSignal) {
    let exists = false;
    try {
      exists = (await stat(job.worktree)).isDirectory();
    } catch (error) {
      if (!hasErrorCode(error, 'ENOENT')) throw error;
    }
    if (exists) {
      const root = (
        await command('git', ['rev-parse', '--show-toplevel'], job.worktree, signal)
      ).stdout.trim();
      const branch = (
        await command('git', ['branch', '--show-current'], job.worktree, signal)
      ).stdout.trim();
      const ancestor = await command(
        'git',
        ['merge-base', '--is-ancestor', job.baseSha, 'HEAD'],
        job.worktree,
        signal,
        { allowFailure: true },
      );
      if (resolve(root) !== resolve(job.worktree) || branch !== job.branch || ancestor.code !== 0)
        throw new Error('Existing worktree does not match the durable job');
      return;
    }
    await mkdir(dirname(job.worktree), { recursive: true, mode: 0o700 });
    const branch = await command(
      'git',
      ['show-ref', '--verify', `refs/heads/${job.branch}`],
      job.root,
      signal,
      { allowFailure: true },
    );
    const args =
      branch.code === 0
        ? ['worktree', 'add', job.worktree, job.branch]
        : ['worktree', 'add', '-b', job.branch, job.worktree, job.baseSha];
    await command('git', args, job.root, signal);
  }
  async commit(job: JobInput, subject: string, signal: AbortSignal): Promise<string> {
    await command('git', ['add', '--all'], job.worktree, signal);
    const changed = await command('git', ['diff', '--cached', '--quiet'], job.worktree, signal, {
      allowFailure: true,
    });
    if (changed.code === 1) await command('git', ['commit', '-m', subject], job.worktree, signal);
    else if (changed.code !== 0) throw new Error(changed.stderr);
    return this.head(job, signal);
  }
  async head(job: JobInput, signal: AbortSignal) {
    return (await command('git', ['rev-parse', 'HEAD'], job.worktree, signal)).stdout.trim();
  }
  async diff(job: JobInput, signal: AbortSignal) {
    return (await command('git', ['diff', job.baseSha, '--'], job.worktree, signal)).stdout;
  }
  async clean(job: JobInput, signal: AbortSignal) {
    if ((await command('git', ['status', '--porcelain'], job.worktree, signal)).stdout.trim())
      throw new Error('Verification or review mutated the worktree');
  }
  async verify(job: JobInput, signal: AbortSignal): Promise<Verification[]> {
    const results: Verification[] = [];
    for (const script of job.config.verify) {
      const result = await command(
        process.execPath,
        supervisedArguments(join(dirname(dirname(job.worktree)), 'executions'), script),
        job.worktree,
        signal,
        { allowFailure: true },
      );
      results.push({
        command: script,
        output: result.stdout + result.stderr,
        exitCode: result.code,
      });
      if (result.code !== 0) break;
    }
    return results;
  }
  async push(job: JobInput, sha: string, signal: AbortSignal) {
    await this.clean(job, signal);
    if ((await this.head(job, signal)) !== sha) throw new Error('HEAD changed after verification');
    await command('git', ['push', 'origin', `HEAD:refs/heads/${job.branch}`], job.worktree, signal);
  }
  async cleanup(job: JobInput, signal?: AbortSignal) {
    const result = await command('git', ['worktree', 'list', '--porcelain'], job.root, signal);
    if (result.stdout.split('\n').includes(`worktree ${job.worktree}`))
      await command('git', ['worktree', 'remove', '--force', job.worktree], job.root, signal);
  }
  async readArtifact(job: JobInput, name: 'blocked.md' | 'pull-request.md'): Promise<string> {
    try {
      return await readFile(join(job.worktree, '.romp', name), 'utf8');
    } catch (error) {
      if (hasErrorCode(error, 'ENOENT')) return '';
      throw error;
    }
  }
  async removeArtifact(job: JobInput, name: 'pull-request.md') {
    await rm(join(job.worktree, '.romp', name), { force: true });
  }
}
