import React, { useEffect, useMemo, useState } from "react";
import { listObservations } from "@/api/queries";
import LineChart, { Series as LineSeries } from "./LineChart";

type Observation = {
  tenant_id?: string;
  id?: string;
  ts?: string;
  service?: string;
  status?: string;
  latency_ms?: number;
  message?: string;
  kind?: string;
};

function safeString(v: unknown): string {
  return String(v ?? "").trim().replaceAll("\u0000", "");
}

function safeNumber(v: unknown): number | null {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : null;
}

function parseTimeMaybe(s: string): number | null {
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

function sortObs(a: Observation, b: Observation): number {
  const ta = parseTimeMaybe(safeString(a.ts));
  const tb = parseTimeMaybe(safeString(b.ts));
  if (ta !== null && tb !== null) {
    if (ta !== tb) return ta - tb;
  } else {
    const sa = safeString(a.ts);
    const sb = safeString(b.ts);
    if (sa !== sb) return sa.localeCompare(sb);
  }
  return safeString(a.id).localeCompare(safeString(b.id));
}

export type LiveMetricsProps = {
  defaultService?: string;
  defaultLimit?: number;
};

export default function LiveMetrics(props: LiveMetricsProps) {
  const [service, setService] = useState<string>(props.defaultService ?? "");
  const [limit, setLimit] = useState<number>(props.defaultLimit ?? 200);
  const [items, setItems] = useState<Observation[]>([]);
  const [error, setError] = useState<string>("");

  useEffect(() => {
    let alive = true;

    async function load() {
      try {
        const res = await listObservations(service || undefined, limit || 200, undefined);
        const list = Array.isArray(res?.items) ? (res.items as Observation[]) : [];
        const sorted = list.slice().sort(sortObs);
        if (alive) {
          setItems(sorted);
          setError("");
        }
      } catch (e: any) {
        if (alive) setError(String(e?.message ?? e));
      }
    }

    load();
    const id = setInterval(load, 5000);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, [service, limit]);

  const latencySeries = useMemo<LineSeries[]>(() => {
    const pts = items
      .filter((o) => {
        const s = safeString(o.service);
        if (!service) return true;
        return s === service;
      })
      .map((o) => {
        const ts = safeString(o.ts);
        const lat = safeNumber(o.latency_ms);
        if (!ts || lat === null) return null;
        return { x: ts, y: lat };
      })
      .filter(Boolean) as { x: string | number; y: number }[];

    return [{ name: "latency_ms", points: pts }];
  }, [items, service]);

  const services = useMemo(() => {
    const set = new Set<string>();
    for (const o of items) {
      const s = safeString(o.service);
      if (s) set.add(s);
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [items]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", gap: 12, alignItems: "center", flexWrap: "wrap" }}>
        <div>
          <label style={{ display: "block", fontSize: 12, opacity: 0.8 }}>Service</label>
          <select
            value={service}
            onChange={(e) => setService(e.target.value)}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", minWidth: 200 }}
          >
            <option value="">(all)</option>
            {services.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>

        <div>
          <label style={{ display: "block", fontSize: 12, opacity: 0.8 }}>Limit</label>
          <input
            type="number"
            value={limit}
            min={1}
            max={5000}
            onChange={(e) => setLimit(Math.max(1, Math.min(5000, Number(e.target.value) || 200)))}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: 120 }}
          />
        </div>

        <div style={{ marginLeft: "auto", fontSize: 12, opacity: 0.75 }}>
          polling every 5s  {items.length} items
        </div>
      </div>

      {error ? (
        <div style={{ padding: 10, border: "1px solid #f2c", borderRadius: 8, color: "#900" }}>
          {error}
        </div>
      ) : null}

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <div style={{ marginBottom: 8, fontWeight: 700 }}>Latency (ms)</div>
        <LineChart series={latencySeries} height={280} xLabel="Time" yLabel="Latency (ms)" maxPoints={1500} />
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <div style={{ marginBottom: 8, fontWeight: 700 }}>Latest observations</div>
        <div style={{ overflowX: "auto" }}>
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                {["ts", "service", "status", "latency_ms", "message"].map((h) => (
                  <th key={h} style={{ textAlign: "left", padding: "8px 6px", borderBottom: "1px solid #eee", fontSize: 12 }}>
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.slice().reverse().slice(0, 200).map((o) => (
                <tr key={`${safeString(o.id)}|${safeString(o.ts)}`}>
                  <td style={{ padding: "6px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{safeString(o.ts)}</td>
                  <td style={{ padding: "6px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{safeString(o.service)}</td>
                  <td style={{ padding: "6px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{safeString(o.status)}</td>
                  <td style={{ padding: "6px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>
                    {safeNumber(o.latency_ms) ?? ""}
                  </td>
                  <td style={{ padding: "6px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{safeString(o.message)}</td>
                </tr>
              ))}
              {items.length === 0 ? (
                <tr>
                  <td colSpan={5} style={{ padding: 10, opacity: 0.7 }}>
                    No observations yet.
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
