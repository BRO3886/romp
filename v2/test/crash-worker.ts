import { openLocalRuntime } from '../src/composition/node.js';
import { TestGitHub, scriptedModels, buildResponses } from './fixture.js';
import { stat } from 'node:fs/promises';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fauxAssistantMessage, fauxToolCall } from '@earendil-works/pi-ai/providers/faux';

const [directory, root, state] = process.argv.slice(2);
if (!directory || !root || !state) throw new Error('Crash worker fixture arguments missing');
const gh = new TestGitHub(directory);
const shellCrash = process.argv[5] === 'shell';
if (!shellCrash)
  gh.afterCreate = async () => {
    process.kill(process.pid, 'SIGKILL');
    await new Promise(() => {});
  };
const { models, faux } = scriptedModels();
faux.setResponses(
  shellCrash
    ? [
        fauxAssistantMessage(
          fauxToolCall('bash', { command: 'touch started; sleep 30; touch escaped' }),
          { stopReason: 'toolUse' },
        ),
      ]
    : buildResponses(),
);
const runtime = await openLocalRuntime(state, gh, { models });
const queued = await runtime.enqueue(root, 17);
if (shellCrash) {
  const job = await runtime.job(queued.id);
  for (let i = 0; i < 200; i++) {
    try {
      await stat(join(job.worktree, 'started'));
      process.kill(process.pid, 'SIGKILL');
      break;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
      await delay(25);
    }
  }
}
await runtime.wait(queued.id);
throw new Error('Crash boundary was not reached');
