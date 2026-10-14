import { expect, test } from 'vitest';
import { appJwt } from '../src/github.js';
function decode(value: string) {
  return Uint8Array.from(atob(value.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
}
function payload(bytes: Uint8Array) {
  let offset = 1;
  const length = bytes[offset];
  if (length === undefined) throw new Error('Missing DER length');
  offset += length > 127 ? 1 + length - 128 : 1;
  return bytes.slice(offset);
}
function rest(bytes: Uint8Array) {
  const length = bytes[1];
  if (length === undefined) throw new Error('Missing DER length');
  let size = length;
  let header = 2;
  if (length > 127) {
    size = 0;
    for (let i = 0; i < length - 128; i++) size = size * 256 + (bytes[2 + i] ?? 0);
    header += length - 128;
  }
  return bytes.slice(header + size);
}
test('GitHub app JWTs sign PKCS8 and GitHub-style PKCS1 keys with the expected claims', async () => {
  const pair = await crypto.subtle.generateKey(
    {
      name: 'RSASSA-PKCS1-v1_5',
      modulusLength: 2048,
      publicExponent: new Uint8Array([1, 0, 1]),
      hash: 'SHA-256',
    },
    true,
    ['sign', 'verify'],
  );
  if (!('privateKey' in pair)) throw new Error('Expected signing key pair');
  const exported = await crypto.subtle.exportKey('pkcs8', pair.privateKey);
  if (!(exported instanceof ArrayBuffer)) throw new Error('Expected PKCS8 bytes');
  const pkcs8 = new Uint8Array(exported);
  const pkcs1 = payload(rest(rest(payload(pkcs8))));
  for (const [format, bytes] of [
    ['PRIVATE KEY', pkcs8],
    ['RSA PRIVATE KEY', pkcs1],
  ] as const) {
    const jwt = await appJwt(
      '123',
      `-----BEGIN ${format}-----\n${btoa(String.fromCharCode(...bytes))}\n-----END ${format}-----`,
      1_000_000,
    );
    const [header, claims, signature] = jwt.split('.');
    if (!header || !claims || !signature) throw new Error('Invalid JWT');
    expect(JSON.parse(new TextDecoder().decode(decode(claims))) as unknown).toEqual({
      iat: 940,
      exp: 1540,
      iss: '123',
    });
    expect(
      await crypto.subtle.verify(
        'RSASSA-PKCS1-v1_5',
        pair.publicKey,
        decode(signature),
        new TextEncoder().encode(`${header}.${claims}`),
      ),
    ).toBe(true);
  }
});
