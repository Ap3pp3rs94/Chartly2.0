import React, { useEffect, useRef, useState } from "react";
import { useLocation } from "react-router-dom";
import StockChart from "@/components/charts/StockChart";

type Point = { t: number; v: number };
type Series = { name: string; points: Point[] };

function parseTime(s: string): number | null {
  if (!s) return null;
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

async function fetchJson(url: string, signal?: AbortSignal): Promise<any> {
  const res = await fetch(url, { signal });
  if (!res.ok) return null;
  return await res.json();
}

export default function Charts() {
  const loc = useLocation();
  const [series, setSeries] = useState<Series | null>(null);
  const [title, setTitle] = useState<string>("Charts");
  const [updatedAt, setUpdatedAt] = useState<string>("");
  const [latencyMs, setLatencyMs] = useState<number | null>(null);
  const [dropped, setDropped] = useState<number>(0);
  const [reportId, setReportId] = useState<string>("crypto-index");
  const [status, setStatus] = useState<string>("");
  const targetRef = useRef<Point[]>([]);
  const rafRef = useRef<number | null>(null);
  const pollRef = useRef<number | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const lastPollRef = useRef<number>(0);
  const initRef = useRef<string>("");

  const params = new URLSearchParams(loc.search);
  const reportParam = params.get("report") || "";
  const profilesParam = params.get("profiles") || "";

  useEffect(() => {
    let mounted = true;
    const initKey = `${reportParam}::${profilesParam}`;
    if (initRef.current === initKey) return;
    initRef.current = initKey;

    if (reportParam) {
      setReportId(reportParam);
      return;
    }

    const profiles = profilesParam
      .split(",")
      .map((p) => p.trim())
      .filter(Boolean);

    if (!profiles.length) {
      setReportId("crypto-index");
      return;
    }

    setStatus("Creating report…");
    fetch("/api/reports", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ profiles, mode: "auto" }),
    })
      .then(async (res) => {
        const data = await res.json().catch(() => ({}));
        const id = data?.id || data?.report_id;
        if (mounted && res.ok && id) {
          setReportId(id);
          setStatus("");
          return;
        }
        if (mounted) setStatus("Failed to create report.");
      })
      .catch(() => {
        if (mounted) setStatus("Failed to create report.");
      });

    return () => {
      mounted = false;
    };
  }, [reportParam, profilesParam]);

  const mergePoints = (prev: Point[], next: Point[]) => {
    if (!prev.length) return next.slice(-600);
    const map = new Map<number, number>();
    for (const p of prev) map.set(p.t, p.v);
    for (const p of next) map.set(p.t, p.v);
    const merged = Array.from(map.entries())
      .map(([t, v]) => ({ t, v }))
      .sort((a, b) => a.t - b.t);
    return merged.slice(-600);
  };

  useEffect(() => {
    const scheduleRender = () => {
      if (rafRef.current != null) return;
      rafRef.current = requestAnimationFrame(() => {
        rafRef.current = null;
        setSeries((prev) => {
          const next = targetRef.current.slice(-600);
          return { name: prev?.name || "CRYPTO_INDEX_USDT", points: next };
        });
      });
    };

    const poll = async () => {
      const now = Date.now();
      if (lastPollRef.current) {
        const delta = now - lastPollRef.current;
        if (delta > 4500) setDropped((d) => d + 1);
      }
      lastPollRef.current = now;

      if (abortRef.current) abortRef.current.abort();
      const ctrl = new AbortController();
      abortRef.current = ctrl;
      const data = await fetchJson(`/api/reports/${encodeURIComponent(reportId)}`, ctrl.signal);
      if (!data) return;
      setTitle(data.title || "Charts");
      setUpdatedAt(data.updated_at || "");
      const s = Array.isArray(data.series) ? data.series[0] : null;
      const pts = Array.isArray(s?.points) ? s.points : [];
      const normalized: Point[] = [];
      for (const p of pts) {
        const t = parseTime(p?.t);
        const v = typeof p?.y === "number" ? p.y : typeof p?.v === "number" ? p.v : null;
        if (t == null || v == null) continue;
        normalized.push({ t, v });
      }
      normalized.sort((a, b) => a.t - b.t);
      targetRef.current = mergePoints(targetRef.current, normalized);
      if (data.updated_at) {
        const ts = parseTime(data.updated_at);
        if (ts != null) setLatencyMs(Math.max(0, Date.now() - ts));
      }
      scheduleRender();
    };

    poll();
    if (pollRef.current) clearInterval(pollRef.current);
    pollRef.current = window.setInterval(poll, 2000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
      if (abortRef.current) abortRef.current.abort();
      if (rafRef.current) cancelAnimationFrame(rafRef.current);
    };
  }, [reportId]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <div style={{ fontSize: 18, fontWeight: 800 }}>{title}</div>
        <div style={{ fontSize: 12, opacity: 0.7, display: "flex", gap: 12 }}>
          {status ? <span>{status}</span> : null}
          {updatedAt ? <span>Updated {updatedAt}</span> : null}
          {latencyMs != null ? <span>Latency {Math.round(latencyMs / 100) / 10}s</span> : null}
          <span>Dropped {dropped}</span>
        </div>
      </div>
      <div style={{ padding: 12, borderRadius: 12, border: "1px solid #1f2a37", background: "#0f141c" }}>
        <StockChart data={series?.points || []} color="#4ea1ff" />
      </div>
    </div>
  );
}
