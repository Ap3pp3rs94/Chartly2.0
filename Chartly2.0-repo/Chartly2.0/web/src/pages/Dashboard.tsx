import React, { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { getHealth } from "@/api/queries";
import LiveMetrics from "@/components/charts/LiveMetrics";
import type { Service } from "@/api/client";

type HealthState = "ok" | "error";

type Row = {
  service: Service;
  label: string;
  state: HealthState;
  detail?: any;
  lastCheckedAt?: string;
};

function nowIso(): string {
  return new Date().toISOString();
}

const servicesOrder: Array<{ service: Service; label: string }> = [
  { service: "gateway", label: "Gateway" },
  { service: "analytics", label: "Analytics" },
  { service: "storage", label: "Storage" },
  { service: "audit", label: "Audit" },
  { service: "auth", label: "Auth" },
  { service: "observer", label: "Observer" },
];

export default function Dashboard() {
  const [rows, setRows] = useState<Row[]>(
    servicesOrder.map((s) => ({ service: s.service, label: s.label, state: "error", detail: undefined, lastCheckedAt: undefined }))
  );

  useEffect(() => {
    let alive = true;

    async function refresh() {
      const next: Row[] = [];
      for (const s of servicesOrder) {
        const r = await getHealth(s.service);
        next.push({
          service: s.service,
          label: s.label,
          state: r.state,
          detail: r.detail,
          lastCheckedAt: nowIso(),
        });
      }
      if (alive) setRows(next);
    }

    refresh();
    const id = setInterval(refresh, 10000);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, []);

  const anyDown = useMemo(() => rows.some((r) => r.state !== "ok"), [rows]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
        <div>
          <h1 style={{ margin: 0, fontSize: 20 }}>Dashboard</h1>
          <div style={{ opacity: 0.75, fontSize: 12 }}>Chartly 2.0 web shell  health + live observer feed</div>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <span style={{ fontSize: 12, opacity: 0.85 }}>
            status: {anyDown ? "DEGRADED" : "OK"}
          </span>
        </div>
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <div style={{ fontWeight: 700, marginBottom: 8 }}>Service Health</div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0, 1fr))", gap: 10 }}>
          {rows.map((r) => (
            <div key={r.service} style={{ border: "1px solid #eee", borderRadius: 8, padding: 10 }}>
              <div style={{ display: "flex", justifyContent: "space-between" }}>
                <strong>{r.label}</strong>
                <span>{r.state === "ok" ? "OK" : "ERR"}</span>
              </div>
              <div style={{ opacity: 0.7, fontSize: 12 }}>{r.lastCheckedAt ? `checked ${r.lastCheckedAt}` : "not checked"}</div>
              {r.state !== "ok" ? (
                <div style={{ color: "#900", fontSize: 12, marginTop: 6 }}>
                  {typeof r.detail === "string" ? r.detail : JSON.stringify(r.detail ?? {})}
                </div>
              ) : null}
            </div>
          ))}
        </div>
      </div>

      <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
        <Link style={linkBtn()} to="/storage">Storage</Link>
        <Link style={linkBtn()} to="/audit">Audit</Link>
        <Link style={linkBtn()} to="/auth">Auth</Link>
        <Link style={linkBtn()} to="/observer">Observer</Link>
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <div style={{ fontWeight: 700, marginBottom: 8 }}>Live Observer Metrics</div>
        <LiveMetrics />
      </div>
    </div>
  );
}

function linkBtn(): React.CSSProperties {
  return {
    padding: "8px 10px",
    borderRadius: 8,
    border: "1px solid #ccc",
    background: "#fff",
    textDecoration: "none",
    color: "#111",
    fontSize: 12,
  };
}
