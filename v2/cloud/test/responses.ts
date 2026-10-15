export function responsesStream(output: readonly unknown[]) {
  const response = {
    id: 'resp_fixture',
    object: 'response',
    status: 'completed',
    output,
    usage: {
      input_tokens: 10,
      output_tokens: 5,
      total_tokens: 15,
      input_tokens_details: { cached_tokens: 0 },
      output_tokens_details: { reasoning_tokens: 0 },
    },
  };
  const events = [
    { type: 'response.created', response: { ...response, status: 'in_progress', output: [] } },
    ...output.flatMap((item, index) => [
      { type: 'response.output_item.added', output_index: index, item },
      { type: 'response.output_item.done', output_index: index, item },
    ]),
    { type: 'response.completed', response },
  ];
  return events
    .map(
      (event, sequence_number) =>
        `event: ${event.type}\ndata: ${JSON.stringify({ ...event, sequence_number })}\n\n`,
    )
    .join('');
}
export const completedMessage = {
  type: 'message',
  id: 'msg_fixture',
  status: 'completed',
  role: 'assistant',
  content: [{ type: 'output_text', text: 'Updated answer.txt', annotations: [] }],
};
