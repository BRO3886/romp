import { z } from 'zod';

export const OpenAIAuth = z.enum(['api-key', 'subscription']);
export const SubscriptionCredential = z
  .object({
    type: z.literal('oauth'),
    access: z.string().min(1).max(32768),
    refresh: z.string().min(1).max(32768),
    expires: z.number().finite(),
    clientId: z
      .string()
      .min(1)
      .refine((id) => id !== 'dynamic_agent_client'),
    scopes: z.array(z.string()).refine((scopes) => scopes.includes('chatgpt.tokens.use.direct')),
  })
  .passthrough();
export type SubscriptionCredential = z.infer<typeof SubscriptionCredential>;
export const SubscriptionImport = z
  .object({
    sessionId: z.uuid(),
    hostId: z.uuid(),
    credential: SubscriptionCredential,
  })
  .strict();
export type SubscriptionImport = z.infer<typeof SubscriptionImport>;
export const SubscriptionStatus = z.object({
  configured: z.boolean(),
  requiresLogin: z.boolean(),
  sessionId: z.string().optional(),
  hostId: z.string().optional(),
  expires: z.number().optional(),
});
