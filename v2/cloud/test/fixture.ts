import type { Submission } from '../src/config.js';
export const config: Submission['repository'] = {
  repo: 'test/repo',
  installationId: 456,
  config: {
    label: 'romp:v2',
    claimedLabel: 'romp:v2:claimed',
    blockedLabel: 'romp:v2:blocked',
    changesRequestedLabel: 'romp:v2:changes-requested',
    base: 'main',
    verify: ['test "$(cat answer.txt)" = 42'],
    model: { provider: 'openai', modelId: 'gpt-6.1-sol' },
    reviewModel: { provider: 'openai', modelId: 'gpt-6.1-sol' },
    review: false,
    maxFixRounds: 0,
    timeoutMs: 1_800_000,
  },
};
declare module 'vitest' {
  interface ProvidedContext {
    runnerUrl: string;
  }
}
export function submission(n = 17): Submission {
  return {
    id: `r123-i${n}`,
    repositoryId: 123,
    repository: config,
    issue: {
      number: n,
      title: 'Change answer',
      body: 'Set answer.txt to 42',
      url: `https://github.com/test/repo/issues/${n}`,
      labels: [],
    },
  };
}
