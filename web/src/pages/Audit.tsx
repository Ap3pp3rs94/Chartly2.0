import React, { useEffect, useMemo, useState } from "react";
import { getHealth, listAuditEvents } from "@/api/queries";
import DataGrid from "@/components/common/DataGrid";

type HealthState = "ok" | "error";

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
}

function parseTimeMaybe(s: string): number | null {
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

function sortEvents(items: any[]): any[] {
  const cp = (items ?? []).slice();
  cp.sort((a, b) => {
    const ta = parseTimeMaybe(safeString(a?.event_ts ?? a?.ts ?? ""));
    const tb = parseTimeMaybe(safeString(b?.event_ts ?? b?.ts ?? ""));
    if (ta !== null && tb !== null && ta !== tb) return ta - tb;

    const sa = safeString(a?.event_ts ?? a?.ts ?? "");
    const sb = safeString(b?.event_ts ?? b?.ts ?? "");
    if (sa !== sb) return sa.localeCompare(sb);

    return safeString(a?.event_id ?? a?.id ?? "").localeCompare(safeString(b?.event_id ?? b?.id ?? ""));
  });
  return cp;
}

export default function Audit() {
  const [health, setHealth] = useState<HealthState>("error");
  const [healthDetail, setHealthDetail] = useState<any>(null);

  const [since, setSince] = useState<string>("");
  const [limit, setLimit] = useState<number>(200);

  const [rows, setRows] = useState<any[]>([]);
  const [error, setError] = useState<string>("");

  async function refresh() {
    try {
      const h = await getHealth("audit");
      setHealth(h.state);
      setHealthDetail(h.detail);

      const res = await listAuditEvents(limit, since || undefined);
      const items = Array.isArray(res?.events) ? res.events : Array.isArray(res?.items) ? res.items : [];
      setRows(sortEvents(items));
      setError("");
    } catch (e: any) {
      setError(String(e?.message ?? e));
    }
  }

  useEffect(() => {
    let alive = true;
    refresh();
    const id = setInterval(() => {
      if (alive) refresh();
    }, 10000);
    return () => {
      alive = false;
      clearInterval(id);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [since, limit]);

  const columns = useMemo(() => {
    // Prefer common fields first, then others inferred by DataGrid anyway.
    return ["event_ts", "event_id", "action", "outcome", "object_key", "request_id", "actor_id", "source", "detail_json"];
  }, []);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h1 style={{ margin: 0, fontSize: 20 }}>Audit</h1>
        <div style={{ opacity: 0.75, fontSize: 12 }}>Audit events (v0)  auto-refresh 10s</div>
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <strong>Service Health</strong>
        <div style={{ marginTop: 6 }}>
          status: {health === "ok" ? "OK" : "ERR"}{" "}
          <span style={{ opacity: 0.7, fontSize: 12 }}>{healthDetail ? JSON.stringify(healthDetail) : ""}</span>
        </div>
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
          <div style={{ fontWeight: 700 }}>Filters</div>

          <div>
            <div style={{ fontSize: 12, opacity: 0.75 }}>since (RFC3339)</div>
            <input
              value={since}
              onChange={(e) => setSince(e.target.value)}
              placeholder="e.g. 2026-01-01T00:00:00Z"
              style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", minWidth: 260 }}
            />
          </div>

          <div>
            <div style={{ fontSize: 12, opacity: 0.75 }}>limit</div>
            <input
              type="number"
              value={limit}
              min={1}
              max={5000}
              onChange={(e) => setLimit(Math.max(1, Math.min(5000, Number(e.target.value) || 200)))}
              style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: 120 }}
            />
          </div>

          <button
            onClick={refresh}
            style={{ padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer", marginLeft: "auto" }}
          >
            Refresh
          </button>
        </div>

        {error ? (
          <div style={{ marginTop: 10, padding: 10, border: "1px solid #f2c", borderRadius: 8, color: "#900" }}>
            {error}
          </div>
        ) : null}
      </div>

      <DataGrid rows={rows} columns={columns} pageSize={25} />
    </div>
  );
}
