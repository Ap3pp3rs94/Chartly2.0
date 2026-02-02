import { Client } from "../src/client";

function env(name: string, fallback = ""): string {
  return process.env[name] ? String(process.env[name]) : fallback;
}

async function main(): Promise<void> {
  const baseUrl = env("CHARTLY_BASE_URL", "http://localhost:8080");
  const tenantId = env("CHARTLY_TENANT_ID", "local");
  const requestId = env("CHARTLY_REQUEST_ID", "req_ts_request_json");

  const c = new Client({ baseUrl, tenantId, requestId });

  // requestJson demonstrates typed decoding (best-effort).
  const health = await c.requestJson<Record<string, unknown>>("GET", "/health");
  // eslint-disable-next-line no-console
  console.log("health keys:", Object.keys(health));

  const ready = await c.requestJson<Record<string, unknown>>("GET", "/ready");
  // eslint-disable-next-line no-console
  console.log("ready keys:", Object.keys(ready));
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error("request-json example failed:", err);
  process.exit(1);
});
