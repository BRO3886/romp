import { z } from 'zod';
import { JobEventSchema, JobSchema, StateSchema, nonEmptyText, recordId } from '../domain/model.js';

export const VERSION = '2.0.0-experimental.1';
export const FRAME_LIMIT = 8 * 1024 * 1024;
const Empty = z.object({}).strict();
const Root = z.object({ root: nonEmptyText }).strict();
const Id = z.object({ id: nonEmptyText }).strict();
const methods = {
  ping: { params: Empty, result: z.object({ version: nonEmptyText }).strict() },
  run: {
    params: Root.extend({ issue: recordId }),
    result: z.object({ id: nonEmptyText, taskId: recordId }).strict(),
  },
  watch: { params: Root, result: nonEmptyText },
  unwatch: { params: Root, result: nonEmptyText },
  state: { params: Empty, result: StateSchema },
  job: { params: Id, result: JobSchema },
  wait: { params: Id, result: JobSchema },
  logs: {
    params: Id,
    result: z.object({ events: z.array(JobEventSchema), task: z.unknown().optional() }).strict(),
  },
  cancel: { params: Id, result: z.object({ cancelled: z.literal(true) }).strict() },
  steer: { params: Id.extend({ content: nonEmptyText }), result: recordId },
  stop: { params: Empty, result: z.object({ suspended: z.literal(true) }).strict() },
};
export const contracts = methods;
const envelope = z.object({ version: z.literal(2), id: z.string() }).strict();
export const RequestSchema = z.discriminatedUnion('method', [
  envelope.extend({ method: z.literal('ping'), params: methods.ping.params }),
  envelope.extend({ method: z.literal('run'), params: methods.run.params }),
  envelope.extend({ method: z.literal('watch'), params: methods.watch.params }),
  envelope.extend({ method: z.literal('unwatch'), params: methods.unwatch.params }),
  envelope.extend({ method: z.literal('state'), params: methods.state.params }),
  envelope.extend({ method: z.literal('job'), params: methods.job.params }),
  envelope.extend({ method: z.literal('wait'), params: methods.wait.params }),
  envelope.extend({ method: z.literal('logs'), params: methods.logs.params }),
  envelope.extend({ method: z.literal('cancel'), params: methods.cancel.params }),
  envelope.extend({ method: z.literal('steer'), params: methods.steer.params }),
  envelope.extend({ method: z.literal('stop'), params: methods.stop.params }),
]);
export type Request = z.infer<typeof RequestSchema>;
export function parseRequest(source: string): Request {
  const value: unknown = JSON.parse(source);
  const header = z.object({ method: z.string() }).passthrough().parse(value);
  if (!Object.hasOwn(methods, header.method)) throw new Error(`Unknown method ${header.method}`);
  return RequestSchema.parse(value);
}
export const ResponseSchema = z.union([
  envelope.extend({ result: z.unknown() }),
  envelope.extend({ error: nonEmptyText }),
]);
