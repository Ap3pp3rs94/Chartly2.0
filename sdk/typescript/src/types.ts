/**
 * Chartly 2.0  TypeScript SDK (v0)
 *
 * Public types for the SDK.
 * Kept separate from implementation to encourage stable imports:
 *   import { Client, APIError } from "chartly";
 *   import type { ClientOptions, RequestOptions, Middleware } from "chartly";
 *
 * No side effects.
 */

export type HttpMethod =
  | "GET"
  | "POST"
  | "PUT"
  | "PATCH"
  | "DELETE"
  | "HEAD"
  | "OPTIONS";

/**
 * Fetch type for mixed environments (browser + Node).
 * Uses globalThis.fetch for widest compatibility.
 */
export type FetchLike = typeof globalThis.fetch;

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
   * Default timeout budget in milliseconds (covers fetch + streaming read).
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
  fetchImpl?: FetchLike;

  /**
   * Optional middleware pipeline (executed in order).
   */
  middleware?: Middleware[];
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

/**
 * Best-effort Chartly envelope shape.
 * If response matches, SDK can decode structured errors (and/or data).
 */
export type ChartlyEnvelope = {
  ok?: boolean;
  error?: ErrorInfo;
  request_id?: string;
  data?: unknown;
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

export type RequestContext = {
  method: HttpMethod;
  url: string;
  path: string;
  headers: Record<string, string>;
  bodyText?: string;
  timeoutMs: number;
};

export type ResponseContext = {
  status: number;
  headers: Headers;
  bodyText: string;
  requestId?: string;
};

export type Middleware = {
  /**
   * Called after request context is constructed and before fetch executes.
   * Middleware may mutate ctx.headers/bodyText/url if desired.
   */
  beforeRequest?: (ctx: RequestContext) => Promise<void> | void;

  /**
   * Called after response text is read (bounded) and before status handling returns.
   */
  afterResponse?: (req: RequestContext, res: ResponseContext) => Promise<void> | void;

  /**
   * Called when request fails (includes APIError + network errors).
   * Middleware should not swallow errors unless it intends to rethrow.
   */
  onError?: (req: RequestContext, err: unknown) => Promise<void> | void;
};
