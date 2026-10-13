import { homedir } from 'node:os';
import { join, resolve } from 'node:path';

export function stateDirectory(): string {
  return resolve(
    process.env['ROMP_V2_STATE'] ??
      join(process.env['XDG_STATE_HOME'] ?? join(homedir(), '.local/state'), 'romp-v2'),
  );
}
