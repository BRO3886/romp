import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { NodeExecutionEnv } from '@earendil-works/pi-durable/env/node';
import type { ExecutionEnv } from '@earendil-works/pi-durable/env';
import { createReadTool, createBashTool, CodingTools } from '@earendil-works/pi-durable/tools';
import { defineExtension, section, type Extension } from '@earendil-works/pi-durable';
import { supervisedArguments, shellQuote } from './supervision.js';

const reads = new Set(['absolutePath', 'joinPath', 'openTextLineReader', 'readTextFile', 'readTextLines',
  'readBinaryFile', 'openBinaryReader', 'fileInfo', 'listDir', 'openDirReader', 'canonicalPath', 'exists', 'cleanup']);
export function executionEnvironment(cwd: string, readOnly: boolean): ExecutionEnv {
  const env = new NodeExecutionEnv({ cwd });
  if (!readOnly) return env;
  return new Proxy(env, {
    get(target, name, receiver) {
      const value: unknown = Reflect.get(target, name, receiver);
      if (typeof value !== 'function') return value;
      if (reads.has(String(name))) return value.bind(target);
      return () => { throw new Error(`Read-only reviewer cannot call ${String(name)}`); };
    },
    set() { throw new Error('Read-only environment cannot be modified'); },
  });
}
const context = section('repository-instructions', async (input) => {
  if (!input.agent.cwd) return '';
  try { return (await readFile(join(input.agent.cwd, 'AGENTS.md'), 'utf8')).slice(0, 128_000); }
  catch (error) { if ((error as NodeJS.ErrnoException).code === 'ENOENT') return ''; throw error; }
});
export const Builder: Extension = defineExtension({ name: 'romp.builder', tools: CodingTools.tools, sections: [context] });
export function supervisedBuilder(directory: string): Extension {
  const bash = createBashTool({ prepare: (execution) => {
    execution.command = `exec ${[process.execPath, ...supervisedArguments(directory, execution.command)].map(shellQuote).join(' ')}`;
  } });
  return defineExtension({ ...Builder, tools: [...Builder.tools!.filter((t) => t.name !== 'bash'), bash] });
}
export const Reviewer: Extension = defineExtension({ name: 'romp.reviewer', tools: [createReadTool()], sections: [context] });
