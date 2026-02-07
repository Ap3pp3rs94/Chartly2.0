import React, { useEffect, useMemo, useState } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import Correlate from "@/pages/Correlate";
import Charts from "@/pages/Charts";
import Dashboard from "@/pages/Dashboard";
import Settings from "@/pages/Settings";
import Discover from "@/pages/Discover";
import { loadWorkspace, setSelectedProfiles } from "@/lib/workspace";
import { getVirtualProfiles, setVirtualProfiles } from "@/lib/storage";

const navItems = [
  { to: "/", label: "Dashboard" },
  { to: "/discover", label: "Add Sources" },
  { to: "/profiles", label: "Sources" },
  { to: "/charts", label: "Explore" },
  { to: "/settings", label: "Settings" }
];

export default function App() {
  const loc = useLocation();
  const [showOnboard, setShowOnboard] = useState(false);
  const [onboardStep, setOnboardStep] = useState(1);
  const [available, setAvailable] = useState<Array<{ id: string; name?: string }>>([]);
  const [picked, setPicked] = useState<string[]>(() => loadWorkspace().selectedProfiles);
  const [hasData, setHasData] = useState(false);
  const [onboardChecked, setOnboardChecked] = useState(false);
  const [pulseOk, setPulseOk] = useState(false);
  const [pulseMsg, setPulseMsg] = useState("Waiting for heartbeat...");

  useEffect(() => {
    const es = new EventSource("/api/events");
    const onHeartbeat = (evt: MessageEvent) => {
      try {
        const data = JSON.parse(evt.data || "{}");
        const services = data?.services || {};
        const allUp = Object.values(services).every((v) => v === "up");
        const ok = data?.status === "ok" && allUp;
        setPulseOk(ok);
        setPulseMsg(ok ? "All services healthy" : "Degraded");
      } catch {
        setPulseOk(false);
        setPulseMsg("Degraded");
      }
    };
    const onError = () => {
      setPulseOk(false);
      setPulseMsg("Disconnected");
    };
    es.addEventListener("heartbeat", onHeartbeat);
    es.addEventListener("error", onError as EventListener);
    return () => {
      es.removeEventListener("heartbeat", onHeartbeat);
      es.removeEventListener("error", onError as EventListener);
      es.close();
    };
  }, []);

  useEffect(() => {
    // Purge any Data.gov artifacts from local storage and selection.
    const isDataGov = (id: string) => /datagov|data-gov/i.test(id);
    const vps = getVirtualProfiles();
    const filteredVps = vps.filter((p) => !isDataGov(p.id));
    if (filteredVps.length !== vps.length) {
      setVirtualProfiles(filteredVps);
    }
    const ws = loadWorkspace();
    const filteredSel = ws.selectedProfiles.filter((id) => !isDataGov(id));
    if (filteredSel.length !== ws.selectedProfiles.length) {
      setSelectedProfiles(filteredSel);
    }

    const ws2 = loadWorkspace();
    setPicked(ws2.selectedProfiles);
    if (!ws2.selectedProfiles.length) setShowOnboard(true);
    else setShowOnboard(false);
  }, []);

  useEffect(() => {
    if (picked.length > 0) setShowOnboard(false);
  }, [picked.length]);

  useEffect(() => {
    // Poll briefly to detect existing data and auto-dismiss onboarding.
    let stopped = false;
    let attempts = 0;
    const tick = async () => {
      if (stopped) return;
      attempts += 1;
      try {
        const r = await fetch("/api/results/summary");
        if (r.ok) {
          const sum = await r.json();
          const total = sum?.total_results ?? 0;
          const profs = Array.isArray(sum?.profiles) ? sum.profiles.map((p: any) => p.profile_id).filter(Boolean) : [];
          if (total > 0) {
            setHasData(true);
            if (picked.length === 0) {
              const seed = (profs.length ? profs : available.map((p) => p.id)).slice(0, 6);
              if (seed.length) {
                setPicked(seed);
                setSelectedProfiles(seed);
              }
            }
            setShowOnboard(false);
            setOnboardChecked(true);
            return;
          }
        }
      } catch {
        // ignore
      }
      try {
        const r2 = await fetch("/api/results?limit=1");
        if (r2.ok) {
          const rows = await r2.json();
          if (Array.isArray(rows) && rows.length > 0) {
            setHasData(true);
            if (picked.length === 0) {
              const seed = available.map((p) => p.id).slice(0, 6);
              if (seed.length) {
                setPicked(seed);
                setSelectedProfiles(seed);
              }
            }
            setShowOnboard(false);
            setOnboardChecked(true);
            return;
          }
        }
      } catch {
        // ignore
      }

      // If profiles exist but no results yet, keep onboarding visible.
      if (attempts >= 6) {
        setOnboardChecked(true);
      } else {
        setTimeout(tick, 2000);
      }
    };
    tick();
    return () => {
      stopped = true;
    };
  }, [available.length, picked.length]);

  useEffect(() => {
    fetch("/api/profiles")
      .then((r) => (r.ok ? r.json() : []))
      .then((data) => {
        const list = Array.isArray(data) ? data : data?.profiles ?? [];
        const normalized: Array<{ id: string; name?: string }> = list.map((p: any) => ({ id: p.id, name: p.name || p.id }));
        setAvailable(normalized);

        // If we have data already, auto-dismiss onboarding and seed watchlist.
        // Prefer summary profile list; fallback to first few available profiles.
        fetch("/api/results/summary")
          .then((r) => (r.ok ? r.json() : null))
          .then((sum) => {
            const total = sum?.total_results ?? 0;
            const profs = Array.isArray(sum?.profiles) ? sum.profiles.map((p: any) => p.profile_id).filter(Boolean) : [];
            if (total > 0) {
              if (picked.length === 0) {
                const seed = (profs.length ? profs : normalized.map((p: { id: string }) => p.id)).slice(0, 6);
                setPicked(seed);
                setSelectedProfiles(seed);
              }
              setShowOnboard(false);
            } else if (picked.length > 0) {
              setShowOnboard(false);
            } else {
              // Fallback check: if any results exist, hide onboarding.
              fetch("/api/results?limit=1")
                .then((r2) => (r2.ok ? r2.json() : []))
                .then((rows) => {
                  if (Array.isArray(rows) && rows.length > 0) {
                    if (picked.length === 0) {
                      const seed = normalized.map((p: { id: string }) => p.id).slice(0, 6);
                      setPicked(seed);
                      setSelectedProfiles(seed);
                    }
                    setShowOnboard(false);
                  }
                })
                .catch(() => {});
            }
          })
          .catch(() => {});
      })
      .catch(() => setAvailable([]));
  }, [picked.length]);

  const sortedAvailable = useMemo(() => {
    const cp = available.slice();
    cp.sort((a, b) => a.id.localeCompare(b.id));
    return cp;
  }, [available]);

  function togglePick(id: string) {
    const next = picked.includes(id) ? picked.filter((x) => x !== id) : [...picked, id];
    next.sort();
    setPicked(next);
  }

  return (
    <div style={{ minHeight: "100vh", display: "grid", gridTemplateColumns: "240px 1fr", background: "#0a0b0d", color: "#f3f4f6" }}>
      <style>{`
        @keyframes pulseGreen { 0%{box-shadow:0 0 0 rgba(50,213,131,0.2);} 70%{box-shadow:0 0 10px rgba(50,213,131,0.8);} 100%{box-shadow:0 0 0 rgba(50,213,131,0.2);} }
        @keyframes pulseRed { 0%{box-shadow:0 0 0 rgba(255,92,122,0.2);} 70%{box-shadow:0 0 10px rgba(255,92,122,0.8);} 100%{box-shadow:0 0 0 rgba(255,92,122,0.2);} }
      `}</style>
      <aside style={{ borderRight: "1px solid #1f2228", padding: 16, display: "flex", flexDirection: "column", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <div style={{ width: 22, height: 22, borderRadius: 2, background: "#0b0c0e", border: "1px solid #2a2d33" }} />
          <div style={{ fontWeight: 800, letterSpacing: 0.5 }}>Chartly</div>
        </div>
        <nav style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {navItems.map((it) => {
            const active = loc.pathname === it.to || (it.to === "/" && loc.pathname === "/dashboard");
            return (
              <Link
                key={it.to}
                to={it.to}
                style={{
                  textDecoration: "none",
                  color: active ? "#f9fafb" : "#9aa0aa",
                  padding: "8px 10px",
                  borderRadius: 2,
                  background: active ? "#15171c" : "transparent",
                  border: "1px solid #1f2228"
                }}
              >
                {it.label}
              </Link>
            );
          })}
        </nav>
        <div style={{ marginTop: "auto", fontSize: 12, opacity: 0.6 }}>Universal live data terminal</div>
      </aside>

      <div style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
        <header
          style={{
            position: "sticky",
            top: 0,
            zIndex: 5,
            borderBottom: "1px solid #1f2228",
            background: "rgba(10,11,13,0.92)",
            padding: "10px 16px",
            display: "flex",
            alignItems: "center",
            gap: 12
          }}
        >
          <input
            placeholder="Search"
            style={{ flex: 1, padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}
          />
          <select style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}>
            <option>Last hour</option>
            <option>Today</option>
            <option>Last 7 days</option>
          </select>
          <div title={pulseMsg} aria-label={pulseMsg} style={{ width: 10, height: 10, borderRadius: 999, background: pulseOk ? "#32d583" : "#ff5c7a", animation: pulseOk ? "pulseGreen 2s infinite" : "pulseRed 2s infinite" }} />
          <Link to="/settings" style={{ textDecoration: "none", color: "#f3f4f6", border: "1px solid #1f2228", padding: "8px 10px", borderRadius: 4, background: "#14161a" }}>
            Settings
          </Link>
        </header>

        <main style={{ padding: 16 }}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/dashboard" element={<Dashboard />} />
            <Route path="/discover" element={<Discover />} />
            <Route path="/profiles" element={<Correlate />} />
            <Route path="/correlate" element={<Correlate />} />
            <Route path="/charts" element={<Charts />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<Dashboard />} />
          </Routes>
        </main>
      </div>

      {showOnboard && !hasData && onboardChecked ? (
        <div
          style={{
            position: "fixed",
            inset: 0,
            background: "rgba(0,0,0,0.65)",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            zIndex: 50
          }}
        >
          <div style={{ width: 720, maxWidth: "92vw", background: "#0f1115", border: "1px solid #1f2228", borderRadius: 6, padding: 20 }}>
            <div style={{ fontWeight: 800, fontSize: 18, letterSpacing: 0.4 }}>Get Live Data in 60 Seconds</div>
            <div style={{ fontSize: 12, opacity: 0.7, marginTop: 6 }}>
              Connect → Pick sources → See your live dashboard.
            </div>

            {onboardStep === 1 ? (
              <div style={{ marginTop: 16 }}>
                <div style={{ fontWeight: 700, marginBottom: 6 }}>Step 1: Connect</div>
                <div style={{ fontSize: 12, opacity: 0.7 }}>
                  Add API keys only if a source requires them.
                </div>
                <div style={{ marginTop: 12, display: "flex", gap: 8 }}>
                  <Link to="/settings" style={{ textDecoration: "none", color: "#f3f4f6", border: "1px solid #1f2228", padding: "8px 10px", borderRadius: 4, background: "#14161a" }}>
                    Open Settings
                  </Link>
                  <button onClick={() => setOnboardStep(2)} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}>
                    Continue
                  </button>
                </div>
              </div>
            ) : null}

            {onboardStep === 2 ? (
              <div style={{ marginTop: 16 }}>
                <div style={{ fontWeight: 700, marginBottom: 6 }}>Step 2: Pick Sources</div>
                <div style={{ maxHeight: 260, overflow: "auto", border: "1px solid #1f2228", borderRadius: 4, padding: 8, background: "#0b0c10" }}>
                  {sortedAvailable.map((p) => (
                    <label key={p.id} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 4px" }}>
                      <input type="checkbox" checked={picked.includes(p.id)} onChange={() => togglePick(p.id)} />
                      <div>
                        <div style={{ fontSize: 13 }}>{p.name || p.id}</div>
                        <div style={{ fontSize: 11, opacity: 0.6 }}>{p.id}</div>
                      </div>
                    </label>
                  ))}
                </div>
                <div style={{ marginTop: 12, display: "flex", gap: 8 }}>
                  <button onClick={() => setPicked(sortedAvailable.map((p) => p.id))} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}>
                    Select All
                  </button>
                  <button onClick={() => setPicked([])} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}>
                    Clear
                  </button>
                  <button onClick={() => setOnboardStep(3)} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}>
                    Continue
                  </button>
                </div>
              </div>
            ) : null}

            {onboardStep === 3 ? (
              <div style={{ marginTop: 16 }}>
                <div style={{ fontWeight: 700, marginBottom: 6 }}>Step 3: See Live Dashboard</div>
                <div style={{ fontSize: 12, opacity: 0.7 }}>We’ll load your live data immediately.</div>
                <div style={{ marginTop: 12, display: "flex", gap: 8 }}>
                  <button
                    onClick={() => {
                      setSelectedProfiles(picked);
                      setShowOnboard(false);
                    }}
                    style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}
                  >
                    Go to Dashboard
                  </button>
                </div>
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}
