export function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function hasErrorCode(error: unknown, code: string): boolean {
  return error instanceof Error && 'code' in error && error.code === code;
}

export function requireValue<T>(value: T | null | undefined, detail: string): T {
  if (value === null || value === undefined) throw new Error(detail);
  return value;
}

export function requireEntry<T>(
  entries: Readonly<Record<string, T>>,
  key: string,
  detail: string,
): T {
  if (!Object.hasOwn(entries, key)) throw new Error(detail);
  return requireValue(entries[key], detail);
}

export function assertNever(value: never): never {
  throw new Error(`Unexpected value: ${String(value)}`);
}
