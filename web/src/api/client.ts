import { z } from "zod";

export type Service = "gateway" | "storage" | "analytics" | "audit" | "auth" | "observer";

export type ClientOptions = {
  tenantId?: string;
  timeoutMs?: number;
};

const serviceBase: Record<Service, string> = {
  gateway: "/api/gateway",
  storage: "/api/storage",
  analytics: "/api/analytics",
  audit: "/api/audit",
  auth: "/api/auth",
  observer: "/api/observer",
};

let reqCounter = 0;

function normalizeTenantId(v: string): string {
  const s = (v ?? "").trim().replaceAll("\u0000", "");
  return s || "local";
}

function mergeHeaders(a?: HeadersInit, b?: HeadersInit): Headers {
  const h = new Headers(a ?? {});
  if (b) {
    const hb = new Headers(b);
    hb.forEach((v, k) => h.set(k, v));
  }
  return h;
}

async function sha256Hex(input: string): Promise<string> {
  const enc = new TextEncoder().encode(input);
  const hash = await crypto.subtle.digest("SHA-256", enc);
  const bytes = new Uint8Array(hash);
  return Array.from(bytes.slice(0, 16))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

async function makeRequestId(method: string, url: string, bodyLen: number, counter: number): Promise<string> {
  return sha256Hex(`${method}|${url}|${bodyLen}|${counter}`);
}

function withTimeout(signal: AbortSignal | undefined, timeoutMs: number): { signal: AbortSignal; cancel: () => void } {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), timeoutMs);

  if (signal) {
    if (signal.aborted) ac.abort();
    else signal.addEventListener("abort", () => ac.abort(), { once: true });
  }

  return {
    signal: ac.signal,
    cancel: () => clearTimeout(t),
  };
}

export class APIClient {
  private tenantId: string;
  private timeoutMs: number;

  constructor(opts?: ClientOptions) {
    this.tenantId = normalizeTenantId(opts?.tenantId ?? "local");
    this.timeoutMs = typeof opts?.timeoutMs === "number" && opts.timeoutMs > 0 ? opts.timeoutMs : 5000;
  }

  setTenant(tenantId: string) {
    this.tenantId = normalizeTenantId(tenantId);
  }

  async request<T>(
    service: Service,
    path: string,
    init?: RequestInit,
    schema?: z.ZodSchema<T>
  ): Promise<T> {
    const base = serviceBase[service];
    if (!base) throw new Error(`unknown service: ${service}`);

    const p = path.startsWith("/") ? path : `/${path}`;
    const url = `${base}${p}`;

    const method = (init?.method ?? "GET").toUpperCase();
    const bodyLen =
      init?.body == null
        ? 0
        : typeof init.body === "string"
        ? init.body.length
        : init.body instanceof Blob
        ? init.body.size
        : init.body instanceof ArrayBuffer
        ? init.body.byteLength
        : 0;

    const counter = ++reqCounter;
    const requestId = await makeRequestId(method, url, bodyLen, counter);

    const headers = mergeHeaders(init?.headers, {
      "X-Tenant-Id": this.tenantId,
      "X-Request-Id": requestId,
      "Accept": "application/json",
    });

    const { signal, cancel } = withTimeout(init?.signal ?? undefined, this.timeoutMs);

    try {
      const res = await fetch(url, {
        ...init,
        method,
        headers,
        signal,
      });

      const contentType = res.headers.get("content-type") ?? "";
      const isJSON = contentType.includes("application/json");

      if (!res.ok) {
        const text = isJSON ? JSON.stringify(await res.json().catch(() => ({}))) : await res.text().catch(() => "");
        throw new Error(`HTTP ${res.status} ${res.statusText}: ${text}`);
      }

      const data: unknown = isJSON ? await res.json() : await res.text();

      if (schema) {
        const parsed = schema.safeParse(data);
        if (!parsed.success) {
          throw new Error(`Schema validation failed: ${parsed.error.message}`);
        }
        return parsed.data;
      }

      return data as T;
    } finally {
      cancel();
    }
  }

  get<T>(service: Service, path: string, schema?: z.ZodSchema<T>, init?: RequestInit) {
    return this.request<T>(service, path, { ...(init ?? {}), method: "GET" }, schema);
  }

  post<T>(service: Service, path: string, body?: unknown, schema?: z.ZodSchema<T>, init?: RequestInit) {
    const headers = mergeHeaders(init?.headers, { "Content-Type": "application/json" });
    return this.request<T>(
      service,
      path,
      { ...(init ?? {}), method: "POST", headers, body: body == null ? undefined : JSON.stringify(body) },
      schema
    );
  }

  put<T>(service: Service, path: string, body?: unknown, schema?: z.ZodSchema<T>, init?: RequestInit) {
    const headers = mergeHeaders(init?.headers, { "Content-Type": "application/json" });
    return this.request<T>(
      service,
      path,
      { ...(init ?? {}), method: "PUT", headers, body: body == null ? undefined : JSON.stringify(body) },
      schema
    );
  }

  del<T>(service: Service, path: string, schema?: z.ZodSchema<T>, init?: RequestInit) {
    return this.request<T>(service, path, { ...(init ?? {}), method: "DELETE" }, schema);
  }
}

export const api = new APIClient();
