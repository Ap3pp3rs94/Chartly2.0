import { Client } from "../src/client";

function env(name: string, fallback = ""): string {
  return process.env[name] ? String(process.env[name]) : fallback;
}

async function main(): Promise<void> {
  const baseUrl = env("CHARTLY_BASE_URL", "http://localhost:8080");
  const tenantId = env("CHARTLY_TENANT_ID", "local");
  const requestId = env("CHARTLY_REQUEST_ID", "req_ts_health");

  const c = new Client({ baseUrl, tenantId, requestId });

  const health = await c.health();
  // eslint-disable-next-line no-console
  console.log("/health:", health);

  const ready = await c.ready();
  // eslint-disable-next-line no-console
  console.log("/ready:", ready);
}

main().catch((err) => {
  // eslint-disable-next-line no-console
  console.error("health example failed:", err);
  process.exit(1);
});
