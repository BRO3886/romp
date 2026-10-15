import { createModels } from '@earendil-works/pi-ai/models';
import { openaiProvider } from '@earendil-works/pi-ai/providers/openai';
import { anthropicProvider } from '@earendil-works/pi-ai/providers/anthropic';
import { FileCredentials } from './auth.js';

export function providerModels(stateDir?: string) {
  const models = createModels(stateDir ? { credentials: new FileCredentials(stateDir) } : {});
  models.setProvider(openaiProvider());
  models.setProvider(anthropicProvider());
  return models;
}
