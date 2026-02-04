import { Client } from "../src/client";
import { APIError } from "../src/types";

function env(name: string, fallback?: string): string {
  const v = (process.env[name] ?? "").trim();
  if (v) return v;
  if (fallback !== undefined) return fallback;
  throw new Error(`Missing required environment variable: ${name}`);
}

function optEnv(name: string): string | undefined {
  const v = (process.env[name] ?? "").trim();
  return v ? v : undefined;
}

async function main() {
  const baseUrl = env("CHARTLY_BASE_URL");
  const tenantId = optEnv("CHARTLY_TENANT_ID");
  const defaultRequestId = optEnv("CHARTLY_REQUEST_ID");

  const client = new Client({
    baseUrl,
    tenantId,
    requestId: defaultRequestId,
  });

  try {
    // 1) The simplest call: health
    const health = await client.health();
    console.log("health:", health);

    // 2) Another simple call: ready (via requestJson)
    const ready = await client.requestJson("GET", "/ready");
    console.log("ready:", ready);

    // 3) Per-request overrides: headers + requestId
    const overridden = await client.requestJson(
      "GET",
      "/health",
      undefined,
      {
        requestId: `example-${Date.now()}`,
        headers: {
          "x-example-mode": "basic_client",
        },
      }
    );
    console.log("health (overridden):", overridden);
  } catch (e: any) {
    if (e instanceof APIError) {
      console.error("APIError:");
      console.error("  status:", e.status);
      console.error("  code:", e.code ?? "(none)");
      console.error("  retryable:", e.retryable ?? false);
      console.error("  requestId:", e.requestId ?? "(unknown)");
      if (e.details !== undefined) {
        console.error("  details:", e.details);
      }
      console.error("  message:", e.message);
      process.exit(2);
    }

    console.error("Error:", String(e));
    process.exit(1);
  }
}

main();
