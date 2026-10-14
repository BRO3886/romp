export type Fetch = typeof fetch;
export async function jsonRequest(
  fetcher: Fetch,
  url: string,
  init: RequestInit = {},
): Promise<unknown> {
  const response = await fetcher(url, {
    ...init,
    redirect: 'error',
    signal: AbortSignal.timeout(30_000),
  });
  if (!response.ok)
    throw new Error(
      `${init.method ?? 'GET'} ${new URL(url).pathname} failed (HTTP ${response.status})`,
    );
  return response.json();
}
