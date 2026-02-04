import { z } from "zod";
import { api, Service } from "./client";

export const HealthSchema = z
  .object({
    ok: z.boolean().optional(),
    ready: z.boolean().optional(),
    status: z.string().optional(),
  })
  .passthrough();

export const GenericListSchema = z
  .object({
    count: z.number(),
    items: z.array(z.unknown()),
  })
  .passthrough();

type HealthState = "ok" | "error";

function buildQuery(params: Record<string, string | number | undefined>): string {
  const keys = Object.keys(params).filter((k) => params[k] !== undefined);
  keys.sort();
  const usp = new URLSearchParams();
  for (const k of keys) {
    const v = params[k];
    if (v === undefined) continue;
    usp.set(k, String(v));
  }
  const s = usp.toString();
  return s ? `?${s}` : "";
}

export async function getHealth(service: Service): Promise<{ service: Service; state: HealthState; detail?: any }> {
  try {
    const detail = await api.get<any>(service, "/health", HealthSchema);
    const ok = detail?.ok !== false;
    return { service, state: ok ? "ok" : "error", detail };
  } catch (e: any) {
    return { service, state: "error", detail: { error: String(e?.message ?? e) } };
  }
}

export async function listAuditEvents(limit?: number, since?: string): Promise<any> {
  const q = buildQuery({ limit, since });
  return api.get<any>("audit", `/v0/events${q}`);
}

export async function listObservations(service?: string, limit?: number, since?: string): Promise<any> {
  const q = buildQuery({ service, limit, since });
  return api.get<any>("observer", `/v0/observe${q}`);
}
