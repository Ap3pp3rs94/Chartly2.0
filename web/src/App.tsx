import React, { useEffect, useMemo, useState } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import Correlate from "@/pages/Correlate";

type HealthState = "unknown" | "ok" | "error";

type ServiceHealth = {
  id: string;
  label: string;
  url: string;
  state: HealthState;
  lastCheckedAt?: string;
  error?: string;
};

function nowIso(): string {
  return new Date().toISOString();
}

async function checkHealth(url: string, timeoutMs: number): Promise<{ ok: boolean; error?: string }> {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, { method: "GET", signal: ac.signal });
    if (!res.ok) return { ok: false, error: `HTTP ${res.status}` };
    return { ok: true };
  } catch (e: any) {
    return { ok: false, error: e?.name === "AbortError" ? "timeout" : String(e) };
  } finally {
    clearTimeout(t);
  }
}

function HealthPanel() {
  const base: ServiceHealth[] = useMemo(
    () => [
      { id: "analytics", label: "Analytics", url: "/api/analytics/health", state: "unknown" },
      { id: "audit", label: "Audit", url: "/api/audit/health", state: "unknown" },
      { id: "auth", label: "Auth", url: "/api/auth/health", state: "unknown" },
      { id: "gateway", label: "Gateway", url: "/api/gateway/health", state: "unknown" },
      { id: "observer", label: "Observer", url: "/api/observer/health", state: "unknown" },
      { id: "storage", label: "Storage", url: "/api/storage/health", state: "unknown" }
    ],
    []
  );

  const [services, setServices] = useState<ServiceHealth[]>(base);

  useEffect(() => {
    let alive = true;

    async function run() {
      const next = await Promise.all(
        services.map(async (s) => {
          const r = await checkHealth(s.url, 2500);
          return {
            ...s,
            state: r.ok ? "ok" : "error",
            lastCheckedAt: nowIso(),
            error: r.ok ? undefined : r.error ?? "error"
          };
        })
      );
      if (alive) setServices(next);
    }

    run();
    const id = setInterval(run, 10000);
    return () => {
      alive = false;
      clearInterval(id);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div style={{ padding: "12px", border: "1px solid #ddd", borderRadius: 8 }}>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 8 }}>
        <strong>Service Health</strong>
        <span style={{ opacity: 0.7, fontSize: 12 }}>auto-refresh 10s</span>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 8 }}>
        {services.map((s) => (
          <div key={s.id} style={{ padding: 8, border: "1px solid #eee", borderRadius: 8 }}>
            <div style={{ display: "flex", justifyContent: "space-between" }}>
              <span>{s.label}</span>
              <span>{s.state === "ok" ? "OK" : s.state === "error" ? "ERR" : "..."}</span>
            </div>
            <div style={{ opacity: 0.7, fontSize: 12 }}>
              {s.lastCheckedAt ? `checked ${s.lastCheckedAt}` : "not checked yet"}
            </div>
            {s.error ? <div style={{ color: "#a00", fontSize: 12 }}>{s.error}</div> : null}
          </div>
        ))}
      </div>
    </div>
  );
}

function Page({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>{title}</h1>
      {children}
    </div>
  );
}

function Dashboard() {
  return (
    <Page title="Dashboard">
      <HealthPanel />
      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <strong>Welcome</strong>
        <div style={{ opacity: 0.75, marginTop: 6 }}>
          This is the Chartly 2.0 web shell. It provides basic navigation and health checks. More UI will be added as
          service APIs mature.
        </div>
      </div>
    </Page>
  );
}

function Services() {
  return (
    <Page title="Services">
      <HealthPanel />
    </Page>
  );
}

function Storage() {
  return <Page title="Storage">Coming soon.</Page>;
}

function Audit() {
  return <Page title="Audit">Coming soon.</Page>;
}

function Auth() {
  return <Page title="Auth">Coming soon.</Page>;
}

function Observer() {
  return <Page title="Observer">Coming soon.</Page>;
}

function NotFound() {
  return <Page title="Not Found">Route not found.</Page>;
}

const navItems = [
  { to: "/", label: "Dashboard" },
  { to: "/services", label: "Services" },
  { to: "/storage", label: "Storage" },
  { to: "/audit", label: "Audit" },
  { to: "/auth", label: "Auth" },
  { to: "/observer", label: "Observer" },
  { to: "/correlate", label: "Correlate" }
];

export default function App() {
  const loc = useLocation();

  return (
    <div style={{ display: "grid", gridTemplateColumns: "220px 1fr", minHeight: "100vh" }}>
      <aside style={{ borderRight: "1px solid #ddd", padding: 12 }}>
        <div style={{ fontWeight: 700, marginBottom: 12 }}>Chartly 2.0</div>
        <nav style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          {navItems.map((it) => {
            const active = loc.pathname === it.to;
            return (
              <Link
                key={it.to}
                to={it.to}
                style={{
                  textDecoration: "none",
                  padding: "8px 10px",
                  borderRadius: 8,
                  background: active ? "#eee" : "transparent",
                  color: "#111"
                }}
              >
                {it.label}
              </Link>
            );
          })}
        </nav>
      </aside>

      <main style={{ padding: 16 }}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/services" element={<Services />} />
          <Route path="/storage" element={<Storage />} />
          <Route path="/audit" element={<Audit />} />
          <Route path="/auth" element={<Auth />} />
          <Route path="/observer" element={<Observer />} />
          <Route path="/correlate" element={<Correlate />} />
          <Route path="*" element={<NotFound />} />
        </Routes>
      </main>
    </div>
  );
}
