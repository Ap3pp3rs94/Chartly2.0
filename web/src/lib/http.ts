export type FetchOptions = {
  timeoutMs?: number;
  retries?: number;
  retryDelayMs?: number;
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

export async function fetchJson<T = any>(url: string, opts: RequestInit = {}, options: FetchOptions = {}): Promise<T> {
  const timeoutMs = options.timeoutMs ?? 8000;
  const retries = options.retries ?? 2;
  const retryDelayMs = options.retryDelayMs ?? 400;

  let lastErr: any = null;

  for (let attempt = 0; attempt <= retries; attempt++) {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), timeoutMs);
    const headers = new Headers(options.headers || {});
    if (opts.headers) {
      const h = new Headers(opts.headers as any);
      h.forEach((v, k) => headers.set(k, v));
    }
    try {
      const res = await fetch(url, { ...opts, headers, signal: options.signal ?? ctrl.signal });
      clearTimeout(t);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const text = await res.text();
      if (!text) return {} as T;
      return JSON.parse(text) as T;
    } catch (err) {
      clearTimeout(t);
      lastErr = err;
      if (attempt < retries) {
        await sleep(retryDelayMs * (attempt + 1));
        continue;
      }
      throw lastErr;
    }
  }
  throw lastErr;
}
