import type { ConversationId, TaskId } from '@earendil-works/pi-durable';
import type { JobOutcome } from '../../domain/model.js';

function assertConversationId(value: number): asserts value is ConversationId {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error('Invalid conversation ID');
}
function assertTaskId(value: number): asserts value is TaskId<JobOutcome> {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error('Invalid task ID');
}
export function conversationId(value: number): ConversationId {
  assertConversationId(value);
  return value;
}
export function taskId(value: number): TaskId<JobOutcome> {
  assertTaskId(value);
  return value;
}
