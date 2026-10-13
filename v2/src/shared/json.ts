export function parseJson(source: string): unknown {
  const value: unknown = JSON.parse(source);
  return value;
}
