import React, { createContext, useContext, useEffect, useRef, useState } from "react";
import { Link, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import Correlate from "@/pages/Correlate";
import Charts from "@/pages/Charts";
import Dashboard from "@/pages/Dashboard";

export type Heartbeat = {
  status: "healthy" | "degraded" | "down" | "unknown";
  ts?: string;
  services?: Record<string, string>;
  counts?: Record<string, number>;
  catalog_hash?: string;
  latest_result_ts?: string;
};

const HeartbeatContext = createContext<Heartbeat | null>(null);
export const useHeartbeat = () => useContext(HeartbeatContext);

const KEY_API = "chartly_api_key";
const KEY_LIMIT = "chartly_default_limit";
const KEY_PREF = "chartly_preferences";

const navItems = [
  { to: "/", label: "Dashboard" },
  { to: "/profiles", label: "Profiles" },
  { to: "/results", label: "Results" },
  { to: "/correlate", label: "Correlate" },
  { to: "/charts", label: "Charts" },
  { to: "/settings", label: "Settings" }
];

function StatusDot({ state, title }: { state: string; title?: string }) {
  const color = state === "up" || state === "healthy" ? "#32d583" : state === "down" ? "#ff5c7a" : "#ffd166";
  return (
    <span
      title={title}
      style={{
        display: "inline-block",
        width: 8,
        height: 8,
        borderRadius: 999,
        background: color,
        boxShadow: `0 0 8px ${color}`,
        marginRight: 6
      }}
    />
  );
}

function SettingsPage() {
  const [apiKey, setApiKey] = useState<string>(() => sessionStorage.getItem(KEY_API) || "");
  const [limit, setLimit] = useState<number>(() => Number(sessionStorage.getItem(KEY_LIMIT) || "100"));
  const [prefs, setPrefs] = useState<{ geo_level: string; time_granularity: string; metric_preference: string }>(() => {
    const raw = localStorage.getItem(KEY_PREF);
    if (!raw) return { geo_level: "auto", time_granularity: "auto", metric_preference: "auto" };
    try {
      return JSON.parse(raw);
    } catch {
      return { geo_level: "auto", time_granularity: "auto", metric_preference: "auto" };
    }
  });
  const nav = useNavigate();

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Settings</h1>
      <div style={{ fontSize: 12, opacity: 0.75 }}>Session only.</div>

      <div style={{ display: "grid", gridTemplateColumns: "240px 1fr", gap: 10, alignItems: "center" }}>
        <label style={{ fontSize: 12, opacity: 0.8 }}>X-API-Key</label>
        <input
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        />
        <label style={{ fontSize: 12, opacity: 0.8 }}>Default limit</label>
        <input
          type="number"
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value) || 100)}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff", width: 160 }}
        />
      </div>

      <div style={{ fontSize: 12, opacity: 0.85 }}>Preferences defaults</div>
      <div style={{ display: "grid", gridTemplateColumns: "240px 1fr", gap: 10, alignItems: "center" }}>
        <label style={{ fontSize: 12, opacity: 0.8 }}>Geo level</label>
        <select
          value={prefs.geo_level}
          onChange={(e) => setPrefs({ ...prefs, geo_level: e.target.value })}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        >
          <option value="auto">auto</option>
          <option value="state">state</option>
          <option value="county">county</option>
          <option value="none">none</option>
        </select>
        <label style={{ fontSize: 12, opacity: 0.8 }}>Time granularity</label>
        <select
          value={prefs.time_granularity}
          onChange={(e) => setPrefs({ ...prefs, time_granularity: e.target.value })}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        >
          <option value="auto">auto</option>
          <option value="year">year</option>
          <option value="month">month</option>
          <option value="day">day</option>
        </select>
        <label style={{ fontSize: 12, opacity: 0.8 }}>Metric preference</label>
        <select
          value={prefs.metric_preference}
          onChange={(e) => setPrefs({ ...prefs, metric_preference: e.target.value })}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        >
          <option value="auto">auto</option>
          <option value="rate">rate</option>
          <option value="count">count</option>
          <option value="total">total</option>
        </select>
      </div>

      <div style={{ display: "flex", gap: 10 }}>
        <button
          onClick={() => {
            sessionStorage.setItem(KEY_API, apiKey);
            sessionStorage.setItem(KEY_LIMIT, String(limit));
            localStorage.setItem(KEY_PREF, JSON.stringify(prefs));
            nav("/");
          }}
          style={{ padding: "8px 12px", borderRadius: 8, background: "#1f6feb", color: "#fff", border: "1px solid #1f6feb", cursor: "pointer" }}
        >
          Save
        </button>
        <button
          onClick={() => {
            sessionStorage.removeItem(KEY_API);
            sessionStorage.removeItem(KEY_LIMIT);
            localStorage.removeItem(KEY_PREF);
            setApiKey("");
            setLimit(100);
            setPrefs({ geo_level: "auto", time_granularity: "auto", metric_preference: "auto" });
          }}
          style={{ padding: "8px 12px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
        >
          Clear
        </button>
      </div>
    </div>
  );
}

function ProfilesPage() {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Profiles</h1>
      <div style={{ fontSize: 12, opacity: 0.75 }}>Managed automatically by backend + profile builder.</div>
    </div>
  );
}

function ResultsPage() {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Results</h1>
      <div style={{ fontSize: 12, opacity: 0.75 }}>Results feed is used by Charts and Correlate automatically.</div>
    </div>
  );
}

export default function App() {
  const loc = useLocation();
  const [hb, setHb] = useState<Heartbeat>({ status: "unknown" });
  const [toast, setToast] = useState<string>("");
  const lastStatus = useRef<string>("unknown");
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    let alive = true;
    let delay = 5000;

    const poll = async () => {
      if (!alive) return;
      try {
        const res = await fetch("/api/heartbeat", { method: "GET" });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as Heartbeat;
        setHb({ ...data, status: data.status || "unknown" });
        if (data.status && data.status !== lastStatus.current) {
          setToast(`Status: ${data.status}`);
          lastStatus.current = data.status;
        }
        delay = 5000;
      } catch {
        setHb((prev) => ({ ...prev, status: "down" }));
        if (lastStatus.current !== "down") {
          setToast("Status: down");
          lastStatus.current = "down";
        }
        delay = Math.min(delay * 2, 60000);
      }
      if (alive) setTimeout(poll, delay);
    };

    const startSSE = () => {
      try {
        const es = new EventSource("/api/events");
        esRef.current = es;
        es.addEventListener("heartbeat", (ev) => {
          try {
            const data = JSON.parse((ev as MessageEvent).data);
            setHb({ ...data, status: data.status || "unknown" });
            if (data.status && data.status !== lastStatus.current) {
              setToast(`Status: ${data.status}`);
              lastStatus.current = data.status;
            }
          } catch {
            // ignore
          }
        });
        es.onerror = () => {
          es.close();
          esRef.current = null;
          poll();
        };
      } catch {
        poll();
      }
    };

    startSSE();
    return () => {
      alive = false;
      if (esRef.current) esRef.current.close();
    };
  }, []);

  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(""), 2500);
    return () => clearTimeout(t);
  }, [toast]);

  const services = hb.services || {};
  const counts = hb.counts || {};

  return (
    <HeartbeatContext.Provider value={hb}>
      <div style={{ minHeight: "100vh", background: "#0b0e13", color: "#fff" }}>
        <header
          style={{
            position: "sticky",
            top: 0,
            zIndex: 10,
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "10px 16px",
            borderBottom: "1px solid #222",
            background: "rgba(10,12,16,0.9)"
          }}
        >
          <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <div style={{ width: 28, height: 28, borderRadius: 8, background: "linear-gradient(135deg,#4ea1ff,#7b5cff,#ff5c7a)" }} />
            <div style={{ fontWeight: 800 }}>Chartly 2.0</div>
          </div>

          <nav style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {navItems.map((it) => {
              const active = loc.pathname === it.to;
              return (
                <Link
                  key={it.to}
                  to={it.to}
                  style={{
                    textDecoration: "none",
                    color: active ? "#fff" : "#a8b3c2",
                    border: "1px solid #222",
                    padding: "6px 10px",
                    borderRadius: 10,
                    background: active ? "rgba(78,161,255,.12)" : "transparent"
                  }}
                >
                  {it.label}
                </Link>
              );
            })}
          </nav>

          <div style={{ display: "flex", alignItems: "center", gap: 12, fontSize: 12 }}>
            <div style={{ display: "flex", alignItems: "center" }}>
              <StatusDot state={hb.status} title={`status: ${hb.status}`} />
              <span>{hb.status}</span>
            </div>
            <div style={{ display: "flex", alignItems: "center" }}>
              <StatusDot state={services.registry || "unknown"} title="registry" />
              <StatusDot state={services.aggregator || "unknown"} title="aggregator" />
              <StatusDot state={services.coordinator || "unknown"} title="coordinator" />
            </div>
            <div style={{ opacity: 0.8 }}>
              P:{counts.profiles ?? 0} D:{counts.drones ?? 0} R:{counts.results ?? 0}
            </div>
            <div style={{ opacity: 0.6 }}>{hb.ts ? hb.ts : ""}</div>
          </div>
        </header>

        <main style={{ padding: 16 }}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/profiles" element={<ProfilesPage />} />
            <Route path="/results" element={<ResultsPage />} />
            <Route path="/correlate" element={<Correlate />} />
            <Route path="/charts" element={<Charts />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Dashboard />} />
          </Routes>
        </main>

        {toast ? (
          <div
            style={{
              position: "fixed",
              right: 12,
              bottom: 12,
              padding: "10px 12px",
              borderRadius: 10,
              border: "1px solid #333",
              background: "rgba(18,24,34,0.95)",
              fontSize: 12
            }}
          >
            {toast}
          </div>
        ) : null}
      </div>
    </HeartbeatContext.Provider>
  );
}
