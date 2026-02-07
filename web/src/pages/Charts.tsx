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
  const targetRef = useRef<Point[]>([]);
  const rafRef = useRef<number | null>(null);
  const pollRef = useRef<number | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const params = new URLSearchParams(loc.search);
  const reportId = params.get("report") || "crypto-index";

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
      targetRef.current = normalized.slice(-600);
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
        <div style={{ fontSize: 12, opacity: 0.7 }}>{updatedAt ? `Updated ${updatedAt}` : ""}</div>
      </div>
      <div style={{ padding: 12, borderRadius: 12, border: "1px solid #1f2a37", background: "#0f141c" }}>
        <StockChart data={series?.points || []} color="#4ea1ff" />
      </div>
    </div>
  );
}
