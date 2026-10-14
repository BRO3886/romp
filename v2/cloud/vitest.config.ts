import { defineConfig } from 'vitest/config';
import { cloudflareTest } from '@cloudflare/vitest-plugin';
import { config } from './test/fixture.js';
export default defineConfig({
  plugins: [
    cloudflareTest((ctx) => ({
      main: './test/worker.ts',
      wrangler: { configPath: './wrangler.jsonc' },
      miniflare: {
        bindings: {
          REPOSITORIES: JSON.stringify([config]),
          CONCURRENCY: '1',
          GITHUB_APP_ID: '123',
          GITHUB_PRIVATE_KEY: 'test-key',
          GITHUB_WEBHOOK_SECRET: 'w'.repeat(32),
          ADMIN_TOKEN: 'a'.repeat(32),
          OPENAI_API_KEY: 'test',
          TEST_RUNNER_URL: ctx.inject<string>('runnerUrl'),
        },
      },
    })),
  ],
  test: {
    include: ['test/**/*.test.ts'],
    testTimeout: 60_000,
    globalSetup: ['./test/docker-server.ts'],
    fileParallelism: false,
  },
});
