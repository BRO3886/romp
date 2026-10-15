import { readFile, writeFile } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
import { createInterface } from 'node:readline/promises';
import { join } from 'node:path';
import { providerModels } from './models.js';
import { hasErrorCode } from '../../shared/errors.js';
import { acquireLock } from './owner-lock.js';

export async function authenticate(
  action: 'login' | 'logout',
  provider: string,
  directory: string,
  agentName?: string,
) {
  const unlock = await acquireLock(directory);
  const models = providerModels(directory);
  const terminal = createInterface({ input: process.stdin, output: process.stdout });
  try {
    if (action === 'logout') {
      await models.logout(provider);
      return;
    }
    const path = join(directory, 'device-id');
    let deviceId: string;
    try {
      deviceId = (await readFile(path, 'utf8')).trim();
    } catch (error) {
      if (!hasErrorCode(error, 'ENOENT')) throw error;
      deviceId = randomUUID();
      await writeFile(path, deviceId, { mode: 0o600, flag: 'wx' });
    }
    await models.login(
      provider,
      'oauth',
      {
        prompt: async (prompt) => {
          if (prompt.type === 'select') {
            process.stdout.write(
              prompt.options.map((o, i) => `${i + 1}. ${o.label}`).join('\n') + '\n',
            );
            const index =
              Number(await terminal.question(`${prompt.message}: `, { signal: prompt.signal })) - 1;
            const selected = prompt.options[index];
            if (!selected) throw new Error('Invalid selection');
            return selected.id;
          }
          return terminal.question(`${prompt.message}: `, { signal: prompt.signal });
        },
        notify: (event) => {
          if (event.type === 'auth_url')
            process.stdout.write(`${event.url}\n${event.instructions ?? ''}\n`);
          else if (event.type === 'device_code')
            process.stdout.write(`${event.verificationUri}\nCode: ${event.userCode}\n`);
          else process.stdout.write(`${event.message}\n`);
        },
      },
      { getDeviceId: () => deviceId, ...(agentName ? { agentName } : {}) },
    );
    process.stdout.write(
      agentName
        ? `Authenticated ${provider} for ${agentName}.\n`
        : `Saved ${provider} login to ${join(directory, 'auth.json')}.\n`,
    );
  } finally {
    terminal.close();
    await unlock();
  }
  return;
}
