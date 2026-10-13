import { defineDoc } from '@earendil-works/pi-durable';
import type { JobState } from '../../domain/model.js';

export const State = defineDoc<JobState>({
  kind: 'romp.state',
  version: 1,
  scope: 'session',
  initial: () => ({ jobs: {}, repositories: {} }),
});
export const Role = defineDoc<{ readOnly: boolean }>({
  kind: 'romp.role',
  version: 1,
  scope: 'conversation',
  history: 'latest',
  fork: 'initial',
  initial: () => ({ readOnly: false }),
});
