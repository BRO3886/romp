import { createModels } from '@earendil-works/pi-ai/models';
import { openaiProvider } from '@earendil-works/pi-ai/providers/openai';
import { anthropicProvider } from '@earendil-works/pi-ai/providers/anthropic';
import { OpenAIAuth } from './subscription-schema.js';
import type { Authentication } from './config.js';
export function cloudModels(
  secrets: Pick<Authentication, 'OPENAI_API_KEY' | 'ANTHROPIC_API_KEY' | 'OPENAI_AUTH'>,
  subscriptionAccess?: () => Promise<string>,
) {
  const mode = OpenAIAuth.parse(secrets.OPENAI_AUTH ?? 'api-key');
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
  const openai = openaiProvider();
  if (mode === 'subscription') {
    openai.auth.apiKey = {
      name: 'Cloud ChatGPT subscription',
      resolve: async () => {
        if (!subscriptionAccess) throw new Error('Cloud subscription access is unavailable');
        return { auth: { apiKey: await subscriptionAccess() }, source: 'ChatGPT subscription' };
      },
    };
  }
  models.setProvider(openai);
  models.setProvider(anthropicProvider());
  return models;
}
export async function checkModels(
  config: { repositories: readonly { config: { model: { provider: string; modelId: string } } }[] },
  secrets: Authentication,
  subscriptionAccess?: () => Promise<string>,
) {
  const models = cloudModels(secrets, subscriptionAccess);
  for (const { config: repository } of config.repositories) {
    const selected = models.getModel(repository.model.provider, repository.model.modelId);
    if (!selected || !(await models.getAuth(selected)))
      throw new Error('Configured model or authentication is unavailable');
  }
}

export function checkModelSelection(config: {
  repositories: readonly { config: { model: { provider: string; modelId: string } } }[];
}) {
  const models = cloudModels({});
  for (const { config: repository } of config.repositories) {
    if (!models.getModel(repository.model.provider, repository.model.modelId))
      throw new Error('Configured model or authentication is unavailable');
  }
}
