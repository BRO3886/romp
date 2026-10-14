import { Type } from 'typebox';
import { defineExtension, defineTool, section } from '@earendil-works/pi-durable';
import type { Sandbox } from './sandbox.js';
const cwd = '/workspace/job';
export function sandboxTools(sandbox: Sandbox) {
  const read = defineTool({
    name: 'read',
    description: 'Read a UTF-8 file in the repository.',
    replay: 'safe',
    parameters: Type.Object({ path: Type.String() }),
    execute: async ({ path }, _api, context) => {
      const result = await sandbox.exec(['cat', '--', path], {
        cwd,
        ...(context.abortSignal ? { signal: context.abortSignal } : {}),
      });
      if (result.exitCode) throw new Error('Cannot read file');
      return { output: [{ type: 'text', text: result.stdout }] };
    },
  });
  const write = defineTool({
    name: 'write',
    description: 'Write a UTF-8 file, creating parent directories.',
    parameters: Type.Object({ path: Type.String(), content: Type.String() }),
    execute: async ({ path, content }, _api, context) => {
      const result = await sandbox.exec(
        [
          'node',
          '-e',
          'const f=require("fs"),p=require("path");f.mkdirSync(p.dirname(process.argv[1]),{recursive:true});f.writeFileSync(process.argv[1],process.argv[2]);',
          path,
          content,
        ],
        { cwd, ...(context.abortSignal ? { signal: context.abortSignal } : {}) },
      );
      if (result.exitCode) throw new Error('Cannot write file');
      await sandbox.checkpoint();
      return { output: [{ type: 'text', text: 'File written' }] };
    },
  });
  const edit = defineTool({
    name: 'edit',
    description:
      'Replace one exact text occurrence in a file. Fails if the text is absent or ambiguous.',
    parameters: Type.Object({
      path: Type.String(),
      oldText: Type.String(),
      newText: Type.String(),
    }),
    execute: async ({ path, oldText, newText }, _api, context) => {
      const result = await sandbox.exec(
        [
          'node',
          '-e',
          'const f=require("fs"),[p,a,b]=process.argv.slice(1),s=f.readFileSync(p,"utf8");if(!a||s.split(a).length!==2)throw Error("Expected exactly one match");f.writeFileSync(p,s.replace(a,b));',
          path,
          oldText,
          newText,
        ],
        { cwd, ...(context.abortSignal ? { signal: context.abortSignal } : {}) },
      );
      if (result.exitCode) throw new Error('Edit requires exactly one match');
      await sandbox.checkpoint();
      return { output: [{ type: 'text', text: 'File edited' }] };
    },
  });
  const bash = defineTool({
    name: 'bash',
    description:
      'Run a shell command in the repository. Commands have a 90-second deadline and bounded output.',
    parameters: Type.Object({ command: Type.String() }),
    execute: async ({ command }, _api, context) => {
      const result = await sandbox.exec(['bash', '-lc', command], {
        cwd,
        ...(context.abortSignal ? { signal: context.abortSignal } : {}),
      });
      await sandbox.checkpoint();
      return {
        output: [
          { type: 'text', text: `${result.stdout}${result.stderr}\nExit code: ${result.exitCode}` },
        ],
      };
    },
  });
  const instructions = section('repository-instructions', async () => {
    const result = await sandbox.exec(
      [
        'node',
        '-e',
        'const f=require("fs");try{process.stdout.write(f.readFileSync("AGENTS.md","utf8").slice(0,128000))}catch(e){if(e.code!=="ENOENT")throw e}',
      ],
      { cwd },
    );
    if (result.exitCode) throw new Error('Cannot read repository instructions');
    return result.stdout;
  });
  return {
    builder: defineExtension({
      name: 'romp.cloud.builder',
      tools: [read, write, edit, bash],
      sections: [instructions],
    }),
    reviewer: defineExtension({
      name: 'romp.cloud.reviewer',
      tools: [read],
      sections: [instructions],
    }),
  };
}
