import React, { useEffect, useMemo, useState } from "react";
import { api } from "@/api/client";

const KEY_TENANT = "chartly.tenantId";
const KEY_TIMEOUT = "chartly.apiTimeoutMs";

function normCollapse(s: string): string {
  const cleaned = String(s ?? "").replaceAll("\u0000", "").trim();
  return cleaned.split(/\s+/g).filter(Boolean).join(" ");
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n));
}

function readTenant(): string {
  const v = localStorage.getItem(KEY_TENANT);
  return normCollapse(v ?? "") || "local";
}

function readTimeout(): number {
  const v = localStorage.getItem(KEY_TIMEOUT);
  const n = Number(v);
  if (!Number.isFinite(n)) return 5000;
  return clamp(Math.floor(n), 500, 60000);
}

export default function Settings() {
  const [tenantId, setTenantId] = useState<string>(() => readTenant());
  const [timeoutMs, setTimeoutMs] = useState<number>(() => readTimeout());
  const [msg, setMsg] = useState<string>("");

  const current = useMemo(() => ({ tenantId: readTenant(), timeoutMs: readTimeout() }), []);

  useEffect(() => {
    // Ensure API client sees current tenant on first load.
    api.setTenant(readTenant());
  }, []);

  function save() {
    const t = normCollapse(tenantId) || "local";
    const tm = clamp(Math.floor(timeoutMs || 5000), 500, 60000);

    localStorage.setItem(KEY_TENANT, t);
    localStorage.setItem(KEY_TIMEOUT, String(tm));

    api.setTenant(t);

    setTenantId(t);
    setTimeoutMs(tm);
    setMsg("Saved.");
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h1 style={{ margin: 0, fontSize: 20 }}>Settings</h1>
        <div style={{ opacity: 0.75, fontSize: 12 }}>Client-side settings (DATA ONLY)</div>
      </div>

      {msg ? (
        <div style={{ padding: 10, border: "1px solid #ddd", borderRadius: 8, background: "#fafafa" }}>{msg}</div>
      ) : null}

      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <div style={{ fontWeight: 700, marginBottom: 8 }}>Current stored values</div>
        <div style={{ fontSize: 12, opacity: 0.85 }}>
          tenantId: <code>{current.tenantId}</code>
        </div>
        <div style={{ fontSize: 12, opacity: 0.85 }}>
          apiTimeoutMs: <code>{current.timeoutMs}</code>
        </div>
      </div>

      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12, display: "flex", flexDirection: "column", gap: 10 }}>
        <div style={{ fontWeight: 700 }}>Edit</div>

        <div style={{ display: "grid", gridTemplateColumns: "240px 1fr", gap: 10, alignItems: "center" }}>
          <label style={{ fontSize: 12, opacity: 0.8 }}>Tenant ID</label>
          <input
            value={tenantId}
            onChange={(e) => setTenantId(e.target.value)}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc" }}
          />

          <label style={{ fontSize: 12, opacity: 0.8 }}>API timeout (ms)</label>
          <input
            type="number"
            value={timeoutMs}
            min={500}
            max={60000}
            onChange={(e) => setTimeoutMs(clamp(Math.floor(Number(e.target.value) || 5000), 500, 60000))}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: 160 }}
          />
        </div>

        <div style={{ display: "flex", justifyContent: "flex-end" }}>
          <button
            onClick={save}
            style={{ padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" }}
          >
            Save
          </button>
        </div>

        <div style={{ fontSize: 12, opacity: 0.75 }}>
          Note: apiTimeoutMs is stored for future use; APIClient currently uses its own default timeout unless extended.
        </div>
      </div>
    </div>
  );
}
