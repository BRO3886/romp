import type { GitHub, ModelAccess, Repositories } from './ports.js';

export async function preflight(
  directory: string,
  dependencies: { repositories: Repositories; github: GitHub; models: ModelAccess },
) {
  const root = await dependencies.repositories.root(directory);
  const config = await dependencies.repositories.config(root);
  await dependencies.github.preflight(root);
  const identity = await dependencies.github.identity(root);
  await dependencies.models.check(config);
  return { root, config, ...identity };
}
