import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useHeartbeat } from "@/App";

type CatalogProfile = { id: string; name: string };

type Catalog = {
  generated_at: string;
  expires_in_seconds: number;
  profiles: CatalogProfile[];
};

type PlanField = { profile_id?: string; path: string; label: string; type: string; confidence: number };

type Plan = {
  intent: string;
  report_type: string;
  profiles: { id: string; name: string }[];
  join_key?: PlanField;
  x?: PlanField;
  y?: PlanField;
  time?: PlanField;
  preferences?: { geo_level: string; time_granularity: string; metric_preference: string };
};

type Recommendation = {
  plan: Plan;
  plan_hash: string;
  confidence: number;
  why: string[];
  fallbacks?: string[];
};

const KEY_PLAN = "chartly.plan";
const KEY_API = "chartly_api_key";
const KEY_PREF = "chartly_preferences";

export default function Correlate() {
  const hb = useHeartbeat();
  const nav = useNavigate();
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [intent, setIntent] = useState("compare");
  const [profileA, setProfileA] = useState<string>("");
  const [profileB, setProfileB] = useState<string>("");
  const [prefs, setPrefs] = useState(() => {
    const raw = localStorage.getItem(KEY_PREF);
    if (!raw) return { geo_level: "auto", time_granularity: "auto", metric_preference: "auto" };
    try {
      return JSON.parse(raw);
    } catch {
      return { geo_level: "auto", time_granularity: "auto", metric_preference: "auto" };
    }
  });
  const [rec, setRec] = useState<Recommendation | null>(null);
  const [name, setName] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    fetchCatalog();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!hb?.catalog_hash) return;
    fetchCatalog(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hb?.catalog_hash]);

  async function fetchCatalog(silent = false) {
    try {
      const res = await fetch("/api/catalog");
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = (await res.json()) as Catalog;
      setCatalog(data);
      if (!profileA && data.profiles[0]) setProfileA(data.profiles[0].id);
      if (!profileB && data.profiles[1]) setProfileB(data.profiles[1].id);
    } catch (e: any) {
      if (!silent) setErr(String(e?.message ?? e));
    }
  }

  async function recommend() {
    setErr("");
    setRec(null);
    const profiles = [profileA, profileB].filter(Boolean);
    const body = { intent, profiles, preferences: prefs };
    const res = await fetch("/api/recommendations", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    if (!res.ok) {
      setErr(`HTTP ${res.status}`);
      return;
    }
    const data = (await res.json()) as Recommendation;
    setRec(data);
  }

  async function saveReport() {
    if (!rec) return;
    const apiKey = sessionStorage.getItem(KEY_API) || "";
    if (!apiKey) {
      setErr("Missing X-API-Key in Settings");
      return;
    }
    const res = await fetch("/api/reports", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-API-Key": apiKey },
      body: JSON.stringify({ name: name || "Report", plan: rec.plan })
    });
    if (!res.ok) {
      setErr(`HTTP ${res.status}`);
      return;
    }
    nav("/");
  }

  function runReport() {
    if (!rec) return;
    sessionStorage.setItem(KEY_PLAN, JSON.stringify(rec.plan));
    nav("/charts");
  }

  const profiles = catalog?.profiles || [];

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Autopilot</h1>
      {err ? <div style={{ fontSize: 12, color: "#ff5c7a" }}>{err}</div> : null}

      <div style={{ display: "grid", gridTemplateColumns: "220px 1fr", gap: 10, alignItems: "center" }}>
        <label style={{ fontSize: 12, opacity: 0.8 }}>Intent</label>
        <select value={intent} onChange={(e) => setIntent(e.target.value)} style={inputStyle}>
          <option value="compare">compare</option>
          <option value="trend">trend</option>
          <option value="rank">rank</option>
          <option value="explain">explain</option>
          <option value="anomaly">anomaly</option>
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Profile A</label>
        <select value={profileA} onChange={(e) => setProfileA(e.target.value)} style={inputStyle}>
          <option value="">auto</option>
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name || p.id}
            </option>
          ))}
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Profile B</label>
        <select value={profileB} onChange={(e) => setProfileB(e.target.value)} style={inputStyle}>
          <option value="">auto</option>
          {profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name || p.id}
            </option>
          ))}
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Geo level</label>
        <select value={prefs.geo_level} onChange={(e) => setPrefs({ ...prefs, geo_level: e.target.value })} style={inputStyle}>
          <option value="auto">auto</option>
          <option value="state">state</option>
          <option value="county">county</option>
          <option value="none">none</option>
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Time granularity</label>
        <select value={prefs.time_granularity} onChange={(e) => setPrefs({ ...prefs, time_granularity: e.target.value })} style={inputStyle}>
          <option value="auto">auto</option>
          <option value="year">year</option>
          <option value="month">month</option>
          <option value="day">day</option>
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Metric preference</label>
        <select value={prefs.metric_preference} onChange={(e) => setPrefs({ ...prefs, metric_preference: e.target.value })} style={inputStyle}>
          <option value="auto">auto</option>
          <option value="rate">rate</option>
          <option value="count">count</option>
          <option value="total">total</option>
        </select>
      </div>

      <div style={{ display: "flex", gap: 10 }}>
        <button onClick={recommend} style={primaryBtn}>Recommend</button>
        <button onClick={runReport} style={secondaryBtn} disabled={!rec}>Run</button>
      </div>

      {rec ? (
        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #222", background: "#10151e" }}>
          <div style={{ fontWeight: 700, marginBottom: 6 }}>Plan</div>
          <div style={{ fontSize: 12 }}>Type: {rec.plan.report_type}</div>
          <div style={{ fontSize: 12 }}>Confidence: {rec.confidence.toFixed(2)}</div>
          <div style={{ fontSize: 12 }}>Join: {rec.plan.join_key?.label || "-"}</div>
          <div style={{ fontSize: 12 }}>X: {rec.plan.x?.label || "-"}</div>
          <div style={{ fontSize: 12 }}>Y: {rec.plan.y?.label || "-"}</div>
          <div style={{ fontSize: 12 }}>Time: {rec.plan.time?.label || "-"}</div>
          <ul style={{ fontSize: 12 }}>
            {rec.why.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>

          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Report name" style={inputStyle} />
            <button onClick={saveReport} style={secondaryBtn}>Save as Report</button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  padding: "8px 10px",
  borderRadius: 8,
  border: "1px solid #333",
  background: "#111",
  color: "#fff"
};

const primaryBtn: React.CSSProperties = {
  padding: "8px 12px",
  borderRadius: 8,
  background: "#1f6feb",
  color: "#fff",
  border: "1px solid #1f6feb",
  cursor: "pointer"
};

const secondaryBtn: React.CSSProperties = {
  padding: "8px 12px",
  borderRadius: 8,
  background: "#222",
  color: "#fff",
  border: "1px solid #333",
  cursor: "pointer"
};
