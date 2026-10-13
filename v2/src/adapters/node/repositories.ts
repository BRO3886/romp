import { readFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import type { Repositories } from '../../application/ports.js';
import { parseConfig } from '../../domain/config.js';
import { command } from './process.js';

export class LocalRepositories implements Repositories {
  async root(directory: string): Promise<string> {
    return resolve(
      (await command('git', ['rev-parse', '--show-toplevel'], directory)).stdout.trim(),
    );
  }
  async config(root: string) {
    return parseConfig(await readFile(join(root, 'romp-v2.toml'), 'utf8'));
  }
}
