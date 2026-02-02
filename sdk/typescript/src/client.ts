/**
 * Chartly 2.0  TypeScript SDK (v0)
 *
 * Thin HTTP client:
 * - Consistent calling patterns + headers (tenant/request id)
 * - W3C trace-context propagation (traceparent/tracestate)
 * - Bounded request/response sizes
 * - Structured error decoding (Chartly envelope)
 *
 * No retries (v0): callers may wrap with their own retry policy.
 */

export type HttpMethod =
  | "GET"
  | "POST"
  | "PUT"
  | "PATCH"
  | "DELETE"
  | "HEAD"
  | "OPTIONS";

export type ClientOptions = {
  baseUrl: string;

  /**
   * Default tenant id to send on every request (if provided).
   * Header: x-chartly-tenant
   */
  tenantId?: string;

  /**
   * Default request id to send (if provided). Callers should override per request.
   * Header: x-request-id
   */
  requestId?: string;

  /**
   * Default timeout in milliseconds (applies per request).
   */
  timeoutMs?: number;

  /**
   * Max request body bytes (default 4MB).
   */
  maxRequestBytes?: number;

  /**
   * Max response body bytes (default 8MB).
   */
  maxResponseBytes?: number;

  /**
   * Optional fetch implementation (for Node <18 or custom environments).
   */
  fetchImpl?: typeof globalThis.fetch;
};

export type RequestOptions = {
  headers?: Record<string, string>;
  tenantId?: string;
  requestId?: string;

  /**
   * Provide a traceparent to propagate. If omitted, one is generated.
   */
  traceparent?: string;

  /**
   * Optional tracestate to propagate.
   */
  tracestate?: string;

  /**
   * Override timeout per request.
   */
  timeoutMs?: number;
};

export type ErrorInfo = {
  code?: string;
  message?: string;
  retryable?: boolean;
  details?: unknown;
};

export class APIError extends Error {
  public readonly status: number;
  public readonly code?: string;
  public readonly retryable?: boolean;
  public readonly details?: unknown;
  public readonly requestId?: string;

  constructor(args: {
    message: string;
    status: number;
    code?: string;
    retryable?: boolean;
    details?: unknown;
    requestId?: string;
  }) {
    super(args.message);
    this.name = "APIError";
    this.status = args.status;
    this.code = args.code;
    this.retryable = args.retryable;
    this.details = args.details;
    this.requestId = args.requestId;
  }
}

/**
 * Chartly response envelope (best-effort).
 * We only rely on it when shape matches.
 */
type ChartlyEnvelope = {
  ok?: boolean;
  error?: ErrorInfo;
  request_id?: string;
  data?: unknown;
};

const DEFAULT_MAX_REQ = 4 * 1024 * 1024; // 4MB
const DEFAULT_MAX_RES = 8 * 1024 * 1024; // 8MB

const MAX_HEADER_VAL_LEN = 1024;
const MAX_EXTRA_HEADERS = 64;

function isAsciiPrintable(ch: string): boolean {
  const c = ch.charCodeAt(0);
  return c >= 0x20 && c < 0x7f; // 0x20-0x7E only
}

function sanitizeHeaderValue(v: string): string {
  const trimmed = (v ?? "").trim().slice(0, MAX_HEADER_VAL_LEN);
  let out = "";
  for (const ch of trimmed) {
    if (isAsciiPrintable(ch)) out += ch;
  }
  return out;
}

function mergeHeaders(
  base: Record<string, string>,
  extra?: Record<string, string>
): Record<string, string> {
  const out: Record<string, string> = { ...base };
  if (!extra) return out;

  let seen = 0;
  for (const [k, v] of Object.entries(extra)) {
    if (!k) continue;
    if (seen >= MAX_EXTRA_HEADERS) break;
    out[k.toLowerCase()] = sanitizeHeaderValue(String(v));
    seen++;
  }
  return out;
}

/**
 * Stable JSON stringify: sorts object keys recursively.
 * Rejects undefined/functions/symbols for deterministic behavior.
 */
function stableStringify(value: unknown): string {
  const normalized = stableNormalize(value);
  return JSON.stringify(normalized);
}

function stableNormalize(value: unknown): unknown {
  if (value === null) return null;

  const t = typeof value;

  // Reject values that cannot be represented in JSON deterministically.
  if (t === "undefined" || t === "function" || t === "symbol") {
    throw new TypeError(`Value of type ${t} is not JSON-serializable`);
  }

  if (t === "string" || t === "number" || t === "boolean") return value;

  if (Array.isArray(value)) {
    return value.map(stableNormalize);
  }

  if (t === "object") {
    const obj = value as Record<string, unknown>;
    const keys = Object.keys(obj).sort();
    const out: Record<string, unknown> = {};
    for (const k of keys) {
      out[k] = stableNormalize(obj[k]);
    }
    return out;
  }

  // bigint not valid JSON; other exotic types rejected above
  throw new TypeError(`Value of type ${t} is not JSON-serializable`);
}

function bytesLen(s: string): number {
  return new TextEncoder().encode(s).byteLength;
}

function joinUrl(baseUrl: string, path: string): string {
  const b = baseUrl.replace(/\/+$/, "");
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${b}${p}`;
}

/**
 * Generate a W3C traceparent header:
 * version-format: 00-<trace-id 32hex>-<span-id 16hex>-<flags 2hex>
 */
function generateTraceparent(sampled = false): string {
  const traceId = randomHex(16); // 16 bytes => 32 hex
  const spanId = randomHex(8); // 8 bytes => 16 hex
  const flags = sampled ? "01" : "00";
  return `00-${traceId}-${spanId}-${flags}`;
}

let _warnedWeakCrypto = false;

function randomHex(bytes: number): string {
  if (typeof crypto !== "undefined" && typeof crypto.getRandomValues === "function") {
    const arr = new Uint8Array(bytes);
    crypto.getRandomValues(arr);
    return Array.from(arr, (b) => b.toString(16).padStart(2, "0")).join("");
  }

  // Weak fallback (format-correct). Warn once.
  if (!_warnedWeakCrypto) {
    _warnedWeakCrypto = true;
    // eslint-disable-next-line no-console
    console.warn("chartly: crypto.getRandomValues unavailable; using weak PRNG for trace IDs");
  }

  let out = "";
  for (let i = 0; i < bytes; i++) {
    const b = Math.floor(Math.random() * 256);
    out += b.toString(16).padStart(2, "0");
  }
  return out;
}

async function readBoundedText(resp: Response, maxBytes: number): Promise<string> {
  const reader = resp.body?.getReader();

  // IMPORTANT: Do not fall back to resp.text() because it can OOM before bounds check.
  if (!reader) {
    throw new Error("Response body streaming not supported; cannot enforce size limit safely");
  }

  const chunks: Uint8Array[] = [];
  let total = 0;

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    if (value) {
      total += value.byteLength;
      if (total > maxBytes) {
        try {
          await reader.cancel();
        } catch {
          // ignore
        }
        throw new Error(`Response body too large (>${maxBytes} bytes)`);
      }
      chunks.push(value);
    }
  }

  const all = new Uint8Array(total);
  let offset = 0;
  for (const c of chunks) {
    all.set(c, offset);
    offset += c.byteLength;
  }

  return new TextDecoder("utf-8", { fatal: false }).decode(all);
}

function extractRequestId(headers: Headers): string | undefined {
  const v =
    headers.get("x-request-id") ||
    headers.get("x-chartly-request-id") ||
    headers.get("request-id");
  return v ? v.trim() : undefined;
}

function tryParseEnvelope(text: string): ChartlyEnvelope | null {
  try {
    const obj = JSON.parse(text) as unknown;
    if (!obj || typeof obj !== "object") return null;
    const env = obj as ChartlyEnvelope;
    if (typeof env.ok === "boolean") return env;
    if (env.error && typeof env.error === "object") return env;
    return null;
  } catch {
    return null;
  }
}

export class Client {
  private readonly baseUrl: string;
  private readonly tenantId?: string;
  private readonly defaultRequestId?: string;
  private readonly timeoutMs: number;
  private readonly maxRequestBytes: number;
  private readonly maxResponseBytes: number;
  private readonly fetchImpl: typeof globalThis.fetch;

  constructor(opts: ClientOptions) {
    if (!opts?.baseUrl) throw new Error("baseUrl is required");

    // Basic URL validation early (fail fast).
    try {
      // eslint-disable-next-line no-new
      new URL(opts.baseUrl);
    } catch {
      throw new Error(`baseUrl must be a valid absolute URL: ${opts.baseUrl}`);
    }

    this.baseUrl = opts.baseUrl;
    this.tenantId = opts.tenantId;
    this.defaultRequestId = opts.requestId;
    this.timeoutMs = opts.timeoutMs ?? 15_000;
    this.maxRequestBytes = opts.maxRequestBytes ?? DEFAULT_MAX_REQ;
    this.maxResponseBytes = opts.maxResponseBytes ?? DEFAULT_MAX_RES;
    this.fetchImpl = opts.fetchImpl ?? globalThis.fetch;
    if (!this.fetchImpl) {
      throw new Error("fetch is not available in this environment; provide fetchImpl");
    }
  }

  public async health(): Promise<unknown> {
    return this.requestJson("GET", "/health");
  }

  public async ready(): Promise<unknown> {
    return this.requestJson("GET", "/ready");
  }

  public async requestJson<T = unknown>(
    method: HttpMethod,
    path: string,
    jsonBody?: unknown,
    options?: RequestOptions
  ): Promise<T> {
    const raw = await this.requestRaw(method, path, jsonBody, options);
    try {
      return JSON.parse(raw) as T;
    } catch (e) {
      throw new Error(`Expected JSON response but received non-JSON (${String(e)})`);
    }
  }

  public async requestRaw(
    method: HttpMethod,
    path: string,
    jsonBody?: unknown,
    options?: RequestOptions
  ): Promise<string> {
    const url = joinUrl(this.baseUrl, path);

    const tenantId = options?.tenantId ?? this.tenantId;
    const requestId = options?.requestId ?? this.defaultRequestId;

    const timeoutMs = options?.timeoutMs ?? this.timeoutMs;

    const traceparent = sanitizeHeaderValue(options?.traceparent ?? generateTraceparent(false));
    const tracestate = options?.tracestate ? sanitizeHeaderValue(options.tracestate) : undefined;

    // Keep all header keys lowercased for consistency.
    const baseHeaders: Record<string, string> = {
      accept: "application/json",
      "content-type": "application/json",
      traceparent,
    };
    if (tracestate) baseHeaders.tracestate = tracestate;
    if (tenantId) baseHeaders["x-chartly-tenant"] = sanitizeHeaderValue(tenantId);
    if (requestId) baseHeaders["x-request-id"] = sanitizeHeaderValue(requestId);

    const headers = mergeHeaders(baseHeaders, options?.headers);

    let bodyText: string | undefined = undefined;
    if (jsonBody !== undefined && method !== "GET" && method !== "HEAD") {
      bodyText = stableStringify(jsonBody);
      const n = bytesLen(bodyText);
      if (n > this.maxRequestBytes) {
        throw new Error(`Request body too large (${n} bytes > ${this.maxRequestBytes} bytes)`);
      }
    }

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), timeoutMs);

    let resp: Response;
    let text: string;

    try {
      resp = await this.fetchImpl(url, {
        method,
        headers,
        body: bodyText,
        signal: controller.signal,
      });

      // CRITICAL FIX: read within the try so timeout remains active during streaming read.
      text = await readBoundedText(resp, this.maxResponseBytes);
    } catch (e: any) {
      if (e?.name === "AbortError") {
        throw new Error(`Request timeout after ${timeoutMs}ms`);
      }
      throw new Error(`Network error calling ${method} ${url}: ${String(e)}`);
    } finally {
      clearTimeout(timeout);
    }

    if (!resp.ok) {
      const reqId = extractRequestId(resp.headers);
      const env = tryParseEnvelope(text);

      if (env?.error) {
        throw new APIError({
          message: env.error.message ?? `HTTP ${resp.status}`,
          status: resp.status,
          code: env.error.code,
          retryable: env.error.retryable,
          details: env.error.details,
          requestId: env.request_id ?? reqId,
        });
      }

      throw new APIError({
        message: `HTTP ${resp.status}: ${text.slice(0, 512)}`,
        status: resp.status,
        requestId: reqId,
      });
    }

    return text;
  }
}
