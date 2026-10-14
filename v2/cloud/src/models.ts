import { createModels } from '@earendil-works/pi-ai/models';
import { openaiProvider } from '@earendil-works/pi-ai/providers/openai';
import { anthropicProvider } from '@earendil-works/pi-ai/providers/anthropic';
import type { CloudConfig, Secrets } from './config.js';
export function cloudModels(secrets: Pick<Secrets, 'OPENAI_API_KEY' | 'ANTHROPIC_API_KEY'>) {
  const models = createModels({
    authContext: {
      env: (name) =>
        Promise.resolve(
          name === 'OPENAI_API_KEY'
            ? secrets.OPENAI_API_KEY
            : name === 'ANTHROPIC_API_KEY'
              ? secrets.ANTHROPIC_API_KEY
              : undefined,
        ),
      fileExists: () => Promise.resolve(false),
    },
  });
  models.setProvider(openaiProvider());
  models.setProvider(anthropicProvider());
  return models;
}
export async function checkModels(config: CloudConfig, secrets: Secrets) {
  const models = cloudModels(secrets);
  for (const { config: repository } of config.repositories) {
    const selected = models.getModel(repository.model.provider, repository.model.modelId);
    if (!selected || !(await models.getAuth(selected)))
      throw new Error('Configured model or authentication is unavailable');
  }
}
