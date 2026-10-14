export async function boundedBody(request: Request, limit = 1_048_576) {
  const reader = request.body?.getReader();
  if (!reader) return new Uint8Array();
  const parts: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const chunk: ReadableStreamReadResult<unknown> = await reader.read();
      if (chunk.done) break;
      const value = chunk.value;
      if (!(value instanceof Uint8Array)) throw new Error('Invalid request stream');
      size += value.byteLength;
      if (size > limit) {
        await reader.cancel();
        throw new Error('Request body too large');
      }
      parts.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const body = new Uint8Array(size);
  let offset = 0;
  for (const part of parts) {
    body.set(part, offset);
    offset += part.length;
  }
  return body;
}
export async function authenticateWebhook(
  body: Uint8Array,
  signature: string | null,
  secret: string,
) {
  if (!secret || !signature || !/^sha256=[0-9a-f]{64}$/.test(signature)) return false;
  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['verify'],
  );
  const digest = Uint8Array.from(signature.slice(7).match(/../g) ?? [], (v) =>
    Number.parseInt(v, 16),
  );
  return crypto.subtle.verify('HMAC', key, digest, body);
}
export async function authenticateAdmin(request: Request, token: string) {
  if (!token) return false;
  const authorization = request.headers.get('authorization') ?? '';
  if (!authorization.startsWith('Bearer ')) return false;
  const supplied = authorization.slice(7);
  const [a, b] = await Promise.all(
    [token, supplied].map((v) => crypto.subtle.digest('SHA-256', new TextEncoder().encode(v))),
  );
  if (!a || !b) return false;
  return crypto.subtle.timingSafeEqual(a, b);
}
