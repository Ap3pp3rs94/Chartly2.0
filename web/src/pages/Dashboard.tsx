import React, { useEffect, useRef, useState } from "react";

type SummaryState = {
  totalResults: number;
  activeProfiles: number;
  lastUpdated: string;
};

const SUMMARY_INTERVAL = 10 * 60 * 1000;

function nowIso() {
  return new Date().toISOString();
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

export default function Dashboard() {
  const [summary, setSummary] = useState<SummaryState>({
    totalResults: 0,
    activeProfiles: 0,
    lastUpdated: nowIso()
  });
  const lastUpdateRef = useRef<string>(nowIso());

  useEffect(() => {
    const refresh = async () => {
      const profiles = await fetchJson("/api/profiles");
      const profileList = Array.isArray(profiles) ? profiles : profiles?.profiles ?? [];
      const activeProfiles = profileList.length;

      const sum = await fetchJson("/api/results/summary");
      let totalResults = sum?.total_results ?? 0;
      if (!totalResults) {
        const wall = await fetchJson("/api/reports/live-crypto-wall");
        totalResults = Array.isArray(wall?.rows) ? wall.rows.length : 0;
      }

      lastUpdateRef.current = nowIso();
      setSummary({
        totalResults,
        activeProfiles,
        lastUpdated: lastUpdateRef.current
      });
    };

    refresh();
    const t = setInterval(refresh, SUMMARY_INTERVAL);
    return () => clearInterval(t);
  }, []);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div style={{ fontSize: 18, fontWeight: 800 }}>Executive Summary</div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0,1fr))", gap: 12 }}>
        <Card title="Total Results" value={summary.totalResults} />
        <Card title="Active Profiles" value={summary.activeProfiles} />
        <Card title="Last Updated" value={summary.lastUpdated} />
      </div>
    </div>
  );
}

function Card({ title, value }: { title: string; value: any }) {
  return (
    <div style={{ padding: 12, borderRadius: 12, border: "1px solid #1f2a37", background: "#0f141c" }}>
      <div style={{ fontSize: 12, opacity: 0.7 }}>{title}</div>
      <div style={{ fontSize: 20, fontWeight: 800 }}>{value ?? ""}</div>
    </div>
  );
}
