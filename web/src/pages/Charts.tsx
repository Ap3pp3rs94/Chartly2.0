import React, { useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router-dom";
import { useHeartbeat } from "@/App";

type PlanField = { profile_id?: string; path: string; label: string; type: string; confidence: number };

type Plan = {
  intent: string;
  report_type: string;
  profiles: { id: string; name: string }[];
  join_key?: PlanField;
  x?: PlanField;
  y?: PlanField;
  time?: PlanField;
};

type ReportFile = { report_id: string; name: string; plan_hash: string; created_at: string; plan: Plan };

type ResultRow = { id: string; drone_id: string; profile_id: string; timestamp: string; data: any };

type Point = { x: number; y: number; label?: string };

const KEY_PLAN = "chartly.plan";

export default function Charts() {
  const hb = useHeartbeat();
  const loc = useLocation();
  const [plan, setPlan] = useState<Plan | null>(null);
  const [rowsA, setRowsA] = useState<ResultRow[]>([]);
  const [rowsB, setRowsB] = useState<ResultRow[]>([]);
  const [err, setErr] = useState<string>("");
  const [updated, setUpdated] = useState<string>("");

  useEffect(() => {
    const url = new URLSearchParams(loc.search);
    const reportId = url.get("report");
    if (reportId) {
      fetch(`/api/reports/${encodeURIComponent(reportId)}`)
        .then((r) => (r.ok ? r.json() : null))
        .then((d: ReportFile | null) => setPlan(d?.plan || null))
        .catch(() => setPlan(null));
      return;
    }
    const raw = sessionStorage.getItem(KEY_PLAN);
    if (raw) {
      try {
        setPlan(JSON.parse(raw));
      } catch {
        setPlan(null);
      }
    }
  }, [loc.search]);

  useEffect(() => {
    if (!plan) return;
    fetchData(plan);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan]);

  useEffect(() => {
    if (!plan) return;
    if (!hb?.latest_result_ts) return;
    setUpdated(hb.latest_result_ts);
    fetchData(plan);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hb?.latest_result_ts]);

  async function fetchData(p: Plan) {
    setErr("");
    try {
      const ids = p.profiles?.map((x) => x.id) || [];
      const a = ids[0];
      const b = ids[1];
      if (a) setRowsA(await fetchResults(a));
      if (b) setRowsB(await fetchResults(b));
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    }
  }

  const content = useMemo(() => {
    if (!plan) return <div style={{ fontSize: 12, opacity: 0.7 }}>No plan found. Run Recommend first.</div>;
    const type = plan.report_type;
    if (type === "correlation_scatter") {
      return renderScatter(plan, rowsA, rowsB);
    }
    if (type === "time_series_line") {
      return renderTimeSeries(plan, rowsA);
    }
    if (type === "categorical_bar") {
      return renderBars(plan, rowsA);
    }
    return renderTable(rowsA);
  }, [plan, rowsA, rowsB]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Charts</h1>
      {updated ? <div style={{ fontSize: 12, opacity: 0.7 }}>Updated: {updated}</div> : null}
      {err ? <div style={{ fontSize: 12, color: "#ff5c7a" }}>Error: {err}</div> : null}
      <div style={{ padding: 12, border: "1px solid #222", borderRadius: 8, background: "#10151e" }}>{content}</div>
    </div>
  );
}

async function fetchResults(profileId: string): Promise<ResultRow[]> {
  const res = await fetch(`/api/results?profile_id=${encodeURIComponent(profileId)}&limit=500`);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const data = await res.json();
  return Array.isArray(data) ? data : [];
}

function expandRows(rows: ResultRow[]): any[] {
  const out: any[] = [];
  for (const r of rows) {
    let d = r.data;
    if (typeof d === "string") {
      try {
        d = JSON.parse(d);
      } catch {
        d = null;
      }
    }
    const items = Array.isArray(d) ? d : d && typeof d === "object" ? [d] : [];
    for (const it of items) out.push(it);
  }
  return out;
}

function getPath(obj: any, path: string): any {
  if (!obj || !path) return undefined;
  const norm = path.replace(/\[(\d+)\]/g, ".$1");
  const parts = norm.split(".").filter(Boolean);
  let cur = obj;
  for (const p of parts) {
    if (cur == null) return undefined;
    const isIndex = /^\d+$/.test(p);
    if (isIndex && Array.isArray(cur)) cur = cur[Number(p)];
    else cur = (cur as any)[p];
  }
  return cur;
}

function toNumber(v: any): number | null {
  if (v == null) return null;
  if (typeof v === "number" && Number.isFinite(v)) return v;
  const n = Number(String(v).replaceAll(",", "").trim());
  return Number.isFinite(n) ? n : null;
}

function renderScatter(plan: Plan, rowsA: ResultRow[], rowsB: ResultRow[]) {
  const join = plan.join_key?.path || "";
  const xPath = plan.x?.path || "";
  const yPath = plan.y?.path || "";
  if (!join || !xPath || !yPath) return <div style={{ fontSize: 12, opacity: 0.7 }}>Missing plan fields.</div>;

  const A = expandRows(rowsA);
  const B = expandRows(rowsB);
  const mapA = new Map<string, any>();
  for (const a of A) {
    const k = getPath(a, join);
    if (k == null) continue;
    if (!mapA.has(String(k))) mapA.set(String(k), a);
  }

  const points: Point[] = [];
  for (const b of B) {
    const k = getPath(b, join);
    if (k == null) continue;
    const a = mapA.get(String(k));
    if (!a) continue;
    const x = toNumber(getPath(a, xPath));
    const y = toNumber(getPath(b, yPath));
    if (x == null || y == null) continue;
    points.push({ x, y, label: String(k) });
  }

  return <ScatterPlot points={points} />;
}

function renderTimeSeries(plan: Plan, rowsA: ResultRow[]) {
  const tPath = plan.time?.path || "";
  const yPath = plan.y?.path || "";
  if (!tPath || !yPath) return <div style={{ fontSize: 12, opacity: 0.7 }}>Missing time or metric.</div>;
  const A = expandRows(rowsA);
  const points: Point[] = [];
  for (const a of A) {
    let tx: number | null = null;
    const raw = getPath(a, tPath);
    if (typeof raw === "string") {
      const t = Date.parse(raw);
      if (!Number.isNaN(t)) tx = t;
    } else {
      const n = toNumber(raw);
      if (n != null) tx = n;
    }
    const y = toNumber(getPath(a, yPath));
    if (tx == null || y == null) continue;
    points.push({ x: tx, y, label: "" });
  }
  points.sort((a, b) => a.x - b.x);
  return <LinePlot points={points} />;
}

function renderBars(plan: Plan, rowsA: ResultRow[]) {
  const join = plan.join_key?.path || "";
  const yPath = plan.y?.path || "";
  if (!join || !yPath) return <div style={{ fontSize: 12, opacity: 0.7 }}>Missing join or metric.</div>;

  const A = expandRows(rowsA);
  const agg = new Map<string, { sum: number; count: number }>();
  for (const a of A) {
    const k = getPath(a, join);
    const y = toNumber(getPath(a, yPath));
    if (k == null || y == null) continue;
    const key = String(k);
    const prev = agg.get(key) || { sum: 0, count: 0 };
    prev.sum += y;
    prev.count += 1;
    agg.set(key, prev);
  }
  const items = Array.from(agg.entries()).map(([k, v]) => ({ key: k, value: v.sum / v.count }));
  items.sort((a, b) => b.value - a.value);
  return <BarPlot items={items.slice(0, 25)} />;
}

function renderTable(rowsA: ResultRow[]) {
  const A = expandRows(rowsA).slice(0, 25);
  if (A.length === 0) return <div style={{ fontSize: 12, opacity: 0.7 }}>No data available.</div>;
  return (
    <table style={{ width: "100%", fontSize: 12 }}>
      <thead>
        <tr style={{ textAlign: "left", opacity: 0.7 }}>
          <th>preview</th>
        </tr>
      </thead>
      <tbody>
        {A.map((row, i) => (
          <tr key={i}>
            <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{JSON.stringify(row).slice(0, 200)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ScatterPlot({ points }: { points: Point[] }) {
  if (points.length === 0) return <div style={{ fontSize: 12, opacity: 0.7 }}>No plottable points.</div>;
  const { minX, maxX, minY, maxY } = bounds(points);
  const w = 720;
  const h = 320;
  const pad = 30;
  const scaleX = (x: number) => pad + ((x - minX) / (maxX - minX || 1)) * (w - pad * 2);
  const scaleY = (y: number) => h - pad - ((y - minY) / (maxY - minY || 1)) * (h - pad * 2);

  return (
    <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h}>
      <rect x={0} y={0} width={w} height={h} fill="#0f141c" stroke="#222" />
      {points.map((p, i) => (
        <circle key={i} cx={scaleX(p.x)} cy={scaleY(p.y)} r={3} fill="#4ea1ff" />
      ))}
    </svg>
  );
}

function LinePlot({ points }: { points: Point[] }) {
  if (points.length === 0) return <div style={{ fontSize: 12, opacity: 0.7 }}>No plottable points.</div>;
  const { minX, maxX, minY, maxY } = bounds(points);
  const w = 720;
  const h = 320;
  const pad = 30;
  const scaleX = (x: number) => pad + ((x - minX) / (maxX - minX || 1)) * (w - pad * 2);
  const scaleY = (y: number) => h - pad - ((y - minY) / (maxY - minY || 1)) * (h - pad * 2);
  const d = points.map((p, i) => `${i === 0 ? "M" : "L"}${scaleX(p.x)},${scaleY(p.y)}`).join(" ");

  return (
    <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h}>
      <rect x={0} y={0} width={w} height={h} fill="#0f141c" stroke="#222" />
      <path d={d} fill="none" stroke="#32d583" strokeWidth={2} />
    </svg>
  );
}

function BarPlot({ items }: { items: { key: string; value: number }[] }) {
  if (items.length === 0) return <div style={{ fontSize: 12, opacity: 0.7 }}>No plottable points.</div>;
  const w = 720;
  const h = 320;
  const pad = 30;
  const maxV = Math.max(...items.map((i) => i.value));
  const barW = (w - pad * 2) / items.length;

  return (
    <svg viewBox={`0 0 ${w} ${h}`} width="100%" height={h}>
      <rect x={0} y={0} width={w} height={h} fill="#0f141c" stroke="#222" />
      {items.map((it, idx) => {
        const x = pad + idx * barW;
        const bh = ((it.value / (maxV || 1)) * (h - pad * 2)) || 0;
        const y = h - pad - bh;
        return <rect key={it.key} x={x} y={y} width={barW - 2} height={bh} fill="#7b5cff" />;
      })}
    </svg>
  );
}

function bounds(points: Point[]) {
  let minX = points[0].x;
  let maxX = points[0].x;
  let minY = points[0].y;
  let maxY = points[0].y;
  for (const p of points) {
    minX = Math.min(minX, p.x);
    maxX = Math.max(maxX, p.x);
    minY = Math.min(minY, p.y);
    maxY = Math.max(maxY, p.y);
  }
  return { minX, maxX, minY, maxY };
}
