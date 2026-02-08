import React, { useEffect, useMemo, useState } from "react";

type ResultRow = {
  id: string;
  profile_id?: string;
  timestamp?: string;
  data?: any;
};

type Profile = { id: string; name?: string };

type ResultsStreamPayload = {
  ts?: string;
  rows?: ResultRow[];
  error?: string;
};

function truncate(v: any, max = 160): string {
  const s = typeof v === "string" ? v : JSON.stringify(v);
  if (!s) return "";
  return s.length > max ? s.slice(0, max) + "…" : s;
}

async function fetchJson(url: string, timeoutMs = 8000): Promise<any> {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(url, { signal: ctrl.signal });
    if (!res.ok) return null;
    return await res.json();
  } catch {
    return null;
  } finally {
    clearTimeout(t);
  }
}

export default function Results() {
  const [rows, setRows] = useState<ResultRow[]>([]);
  const [lastUpdated, setLastUpdated] = useState<string>("");
  const [status, setStatus] = useState<string>("Connecting…");
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [profileId, setProfileId] = useState<string>("");

  const mergeRows = (prev: ResultRow[], next: ResultRow[]) => {
    const map = new Map<string, ResultRow>();
    for (const row of prev) {
      if (row?.id) map.set(row.id, row);
    }
    for (const row of next) {
      if (row?.id) map.set(row.id, row);
    }
    const merged = Array.from(map.values());
    merged.sort((a, b) => {
      const at = a.timestamp ? Date.parse(a.timestamp) : 0;
      const bt = b.timestamp ? Date.parse(b.timestamp) : 0;
      return bt - at;
    });
    return merged.slice(0, 200);
  };

  useEffect(() => {
    fetchJson("/api/profiles").then((data) => {
      const list = Array.isArray(data) ? data : data?.profiles ?? [];
      const normalized = list.map((p: any) => ({ id: p.id, name: p.name || p.id }));
      normalized.sort((a: Profile, b: Profile) => a.id.localeCompare(b.id));
      setProfiles(normalized);
    });
  }, []);

  useEffect(() => {
    const params = new URLSearchParams();
    params.set("limit", "50");
    if (profileId) params.set("profile_id", profileId);

    fetchJson(`/api/results?${params.toString()}`).then((data) => {
      if (Array.isArray(data)) setRows(data);
    });

    const es = new EventSource(`/api/results/stream?${params.toString()}`);
    const onResults = (evt: MessageEvent) => {
      try {
        const payload = JSON.parse(evt.data || "{}") as ResultsStreamPayload;
        if (Array.isArray(payload.rows)) {
          setRows((prev) => mergeRows(prev, payload.rows || []));
          setLastUpdated(payload.ts || new Date().toISOString());
          setStatus(payload.error ? "Degraded" : "Live");
        }
      } catch {
        setStatus("Degraded");
      }
    };
    const onError = () => setStatus("Disconnected");

    es.addEventListener("results", onResults as EventListener);
    es.addEventListener("error", onError as EventListener);
    return () => {
      es.removeEventListener("results", onResults as EventListener);
      es.removeEventListener("error", onError as EventListener);
      es.close();
    };
  }, [profileId]);

  const display = useMemo(() => rows.slice(0, 50), [rows]);

  return (
    <div style={{ display: "grid", gap: 12 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <div style={{ fontSize: 18, fontWeight: 800 }}>Results</div>
        <div style={{ fontSize: 12, opacity: 0.7 }}>{status} {lastUpdated ? `• ${lastUpdated}` : ""}</div>
      </div>
      <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
        <span style={{ fontSize: 12, opacity: 0.7 }}>Profile</span>
        <select value={profileId} onChange={(e) => setProfileId(e.target.value)} style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #1f2a37", background: "#0b0c10", color: "#f3f4f6" }}>
          <option value="">All profiles</option>
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>{p.name || p.id}</option>
          ))}
        </select>
      </div>
      <div style={{ border: "1px solid #1f2228", borderRadius: 8, background: "#0f1115", overflow: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
          <thead>
            <tr>
              <th style={{ textAlign: "left", padding: "8px 10px", borderBottom: "1px solid #1f2228" }}>Profile</th>
              <th style={{ textAlign: "left", padding: "8px 10px", borderBottom: "1px solid #1f2228" }}>Timestamp</th>
              <th style={{ textAlign: "left", padding: "8px 10px", borderBottom: "1px solid #1f2228" }}>Summary</th>
            </tr>
          </thead>
          <tbody>
            {display.map((r) => (
              <tr key={r.id}>
                <td style={{ padding: "6px 10px", borderBottom: "1px solid #14161a" }}>{r.profile_id || "—"}</td>
                <td style={{ padding: "6px 10px", borderBottom: "1px solid #14161a" }}>{r.timestamp || ""}</td>
                <td style={{ padding: "6px 10px", borderBottom: "1px solid #14161a" }}>{truncate(r.data)}</td>
              </tr>
            ))}
            {display.length === 0 ? (
              <tr>
                <td colSpan={3} style={{ padding: "10px", opacity: 0.6 }}>
                  No results yet.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}
