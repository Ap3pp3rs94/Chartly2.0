import { Client } from "../src/client";
import type { Middleware, RequestContext, ResponseContext } from "../src/types";

function env(name: string, fallback = ""): string {
  return process.env[name] ? String(process.env[name]) : fallback;
}

const loggingMiddleware: Middleware = {
  beforeRequest: (ctx: RequestContext) => {
    // eslint-disable-next-line no-console
    console.log("->", ctx.method, ctx.url);
  },
  afterResponse: (_req: RequestContext, res: ResponseContext) => {
    // eslint-disable-next-line no-console
    console.log("<-", res.status, "bytes=", res.bodyText.length);
  },
  onError: (req: RequestContext, err: unknown) => {
    // eslint-disable-next-line no-console
    console.error("!!", req.method, req.url, err);
  },
};

async function main(): Promise<void> {
  const baseUrl = env("CHARTLY_BASE_URL", "http://localhost:8080");
  const tenantId = env("CHARTLY_TENANT_ID", "local");
  const requestId = env("CHARTLY_REQUEST_ID", "req_ts_middleware");

  const c = new Client({ baseUrl, tenantId, requestId, middleware: [loggingMiddleware] });

  await c.health();
  await c.ready();
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error("middleware example failed:", err);
  process.exit(1);
});
