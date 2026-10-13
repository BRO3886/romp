import { randomUUID } from 'node:crypto';
import { mkdir, realpath } from 'node:fs/promises';
import { join } from 'node:path';
import { createModels, type Models } from '@earendil-works/pi-ai/models';
import { openaiProvider } from '@earendil-works/pi-ai/providers/openai';
import { anthropicProvider } from '@earendil-works/pi-ai/providers/anthropic';
import { defineExtension, createRegistry, Harness } from '@earendil-works/pi-durable';
import { openNodeSqliteStorage } from '@earendil-works/pi-durable/storage/sqlite/node';
import { JobService } from '../application/job-service.js';
import { preflight as resolvePreflight } from '../application/preflight.js';
import type { GitHub, ModelAccess } from '../application/ports.js';
import { FileCredentials } from '../adapters/node/auth.js';
import { Reviewer, executionEnvironment, supervisedBuilder } from '../adapters/node/environment.js';
import { Git } from '../adapters/node/git.js';
import { LocalRepositories } from '../adapters/node/repositories.js';
import { recoverShells } from '../adapters/node/supervision.js';
import { context } from '../adapters/pi/context.js';
import { deliveryTask } from '../adapters/pi/delivery.js';
import { State, Role } from '../adapters/pi/documents.js';
import { PiJobExecution } from '../adapters/pi/job-execution.js';
import { message, requireValue } from '../shared/errors.js';

export function providerModels(stateDir?: string) {
  const models = createModels(stateDir ? { credentials: new FileCredentials(stateDir) } : {});
  models.setProvider(openaiProvider());
  models.setProvider(anthropicProvider());
  return models;
}
function modelAccess(models: Models): ModelAccess {
  return {
    async check(config) {
      for (const selected of [config.model, ...(config.review ? [config.reviewModel] : [])]) {
        const model = models.getModel(selected.provider, selected.modelId);
        if (!model)
          throw new Error(
            `Unknown model ${selected.provider}/${selected.modelId}; use models to list supported IDs`,
          );
        if (!(await models.getAuth(model)))
          throw new Error(`No authentication configured for ${selected.provider}`);
      }
    },
  };
}
export function preflight(root: string, github: GitHub, models: Models) {
  return resolvePreflight(root, {
    repositories: new LocalRepositories(),
    github,
    models: modelAccess(models),
  });
}
export async function openLocalRuntime(
  stateDir: string,
  github: GitHub,
  options: { models?: Models; width?: number } = {},
): Promise<JobService> {
  const width = options.width ?? 1;
  if (!Number.isSafeInteger(width) || width < 1 || width > 32)
    throw new Error('Daemon width must be between 1 and 32');
  await mkdir(stateDir, { recursive: true, mode: 0o700 });
  stateDir = await realpath(stateDir);
  await recoverShells(join(stateDir, 'executions'));
  const models = options.models ?? providerModels(stateDir);
  const workspace = new Git();
  const registry = createRegistry();
  const builder = supervisedBuilder(join(stateDir, 'executions'));
  const task = deliveryTask(github, workspace, { builder, reviewer: Reviewer });
  registry.install(builder);
  registry.install(Reviewer);
  registry.install(defineExtension({ name: 'romp.delivery', tasks: [task] }));
  const harness = await Harness.open(
    await openNodeSqliteStorage(join(stateDir, 'agent.sqlite')),
    {
      models,
      registry,
      env: async ({ cwd, conversationId, read }) => {
        if (!cwd) throw new Error('Agent working directory is missing');
        const role = requireValue(
          await read.snapshot(Role, conversationId, context),
          'Agent execution role is missing',
        );
        return executionEnvironment(cwd, role.readOnly);
      },
      settings: {
        extensions: [],
        toolExecution: 'sequential',
        stream: { timeoutMs: 120_000, maxRetries: 1 },
      },
      onReport: (error) => process.stderr.write(`Pi Durable: ${message(error)}\n`),
    },
    context,
  );
  try {
    await harness.commit(async (tx) => {
      await tx.doc(State);
    }, context);
    const service = new JobService({
      execution: new PiJobExecution(harness, task),
      github,
      models: modelAccess(models),
      repositories: new LocalRepositories(),
      workspace,
      width,
      now: Date.now,
      newId: randomUUID,
      worktreePath: (id) => join(stateDir, 'worktrees', id),
    });
    await service.reconcile();
    harness.resume();
    return service;
  } catch (error) {
    await harness.close(context);
    throw error;
  }
}
