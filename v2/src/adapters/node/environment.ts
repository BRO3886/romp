import { hasErrorCode } from '../../shared/errors.js';
import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node';
import type { ExecutionEnv } from '@earendil-works/pi-durable/env';
import { createReadTool, createBashTool, CodingTools } from '@earendil-works/pi-durable/tools';
import { defineExtension, section, type Extension } from '@earendil-works/pi-durable';
import { supervisedArguments, shellQuote } from './supervision.js';

function deny(operation: string): Promise<never> {
  return Promise.reject(new Error(`Read-only reviewer cannot call ${operation}`));
}
export function executionEnvironment(cwd: string, readOnly: boolean): ExecutionEnv {
  const env = new NodeExecutionEnv({ cwd });
  if (!readOnly) return env;
  return Object.freeze({
    id: env.id,
    cwd: env.cwd,
    absolutePath: env.absolutePath.bind(env),
    joinPath: env.joinPath.bind(env),
    openTextLineReader: env.openTextLineReader.bind(env),
    readTextFile: env.readTextFile.bind(env),
    readTextLines: env.readTextLines.bind(env),
    readBinaryFile: env.readBinaryFile.bind(env),
    openBinaryReader: env.openBinaryReader.bind(env),
    fileInfo: env.fileInfo.bind(env),
    listDir: env.listDir.bind(env),
    openDirReader: env.openDirReader.bind(env),
    canonicalPath: env.canonicalPath.bind(env),
    exists: env.exists.bind(env),
    cleanup: env.cleanup.bind(env),
    watch: () => deny('watch'),
    exec: () => deny('exec'),
    writeFile: () => deny('writeFile'),
    appendFile: () => deny('appendFile'),
    truncateFile: () => deny('truncateFile'),
    flushFile: () => deny('flushFile'),
    renameFile: () => deny('renameFile'),
    createDir: () => deny('createDir'),
    remove: () => deny('remove'),
    createTempDir: () => deny('createTempDir'),
    createTempFile: () => deny('createTempFile'),
  });
}
const context = section('repository-instructions', async (input) => {
  if (!input.agent.cwd) return '';
  try {
    return (await readFile(join(input.agent.cwd, 'AGENTS.md'), 'utf8')).slice(0, 128_000);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return '';
    throw error;
  }
});
export const Builder: Extension = defineExtension({
  name: 'romp.builder',
  tools: CodingTools.tools ?? [],
  sections: [context],
});
export function supervisedBuilder(directory: string): Extension {
  const bash = createBashTool({
    prepare: (execution) => {
      execution.command = `exec ${[process.execPath, ...supervisedArguments(directory, execution.command)].map(shellQuote).join(' ')}`;
    },
  });
  return defineExtension({
    ...Builder,
    tools: [...(Builder.tools ?? []).filter((t) => t.name !== 'bash'), bash],
  });
}
export const Reviewer: Extension = defineExtension({
  name: 'romp.reviewer',
  tools: [createReadTool()],
  sections: [context],
});
