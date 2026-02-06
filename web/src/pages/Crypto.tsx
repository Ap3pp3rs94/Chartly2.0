import React, { useEffect, useMemo, useState } from "react";

type CryptoRow = {
  symbol: string;
  timestamp: string;
  open: number;
  close: number;
  high: number;
  low: number;
  volume: number;
  quote_volume: number;
  pct_change: number;
};

type Direction = "up" | "down";

const DEFAULT_LIMIT = 25;
const DEFAULT_MIN_VOL = 1000;
const DEFAULT_SUFFIX = "USDT";

function fmtNum(v: number, digits = 2) {
  if (!Number.isFinite(v)) return "-";
  const abs = Math.abs(v);
  if (abs >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(2)}B`;
  if (abs >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`;
  if (abs >= 1_000) return `${(v / 1_000).toFixed(2)}K`;
  return v.toFixed(digits);
}

export default function Crypto() {
  const [rows, setRows] = useState<CryptoRow[]>([]);
  const [direction, setDirection] = useState<Direction>("up");
  const [limit, setLimit] = useState<number>(DEFAULT_LIMIT);
  const [minVol, setMinVol] = useState<number>(DEFAULT_MIN_VOL);
  const [suffix, setSuffix] = useState<string>(DEFAULT_SUFFIX);
  const [refreshMs, setRefreshMs] = useState<number>(2000);
  const [paused, setPaused] = useState<boolean>(false);
  const [err, setErr] = useState<string>("");
  const [lastUpdated, setLastUpdated] = useState<string>("");

  const params = useMemo(() => {
    const p = new URLSearchParams();
    p.set("limit", String(Math.max(1, Math.min(200, limit || DEFAULT_LIMIT))));
    p.set("direction", direction);
    if (suffix.trim()) p.set("suffix", suffix.trim());
    if (Number.isFinite(minVol) && minVol > 0) p.set("min_vol", String(minVol));
    return p.toString();
  }, [direction, limit, minVol, suffix]);

  useEffect(() => {
    let alive = true;
    let timer: any;

    const tick = async () => {
      if (!alive || paused) return;
      try {
        const res = await fetch(`/api/crypto/top?${params}`);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as CryptoRow[];
        if (alive) {
          setRows(Array.isArray(data) ? data : []);
          setErr("");
          setLastUpdated(new Date().toISOString());
        }
      } catch (e: any) {
        if (alive) setErr(String(e?.message ?? e));
      } finally {
        if (alive && !paused) timer = setTimeout(tick, refreshMs);
      }
    };

    tick();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [params, refreshMs, paused]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Crypto Live</h1>
      <div style={{ fontSize: 12, opacity: 0.7 }}>
        Streaming top movers from Binance mini-tickers. Refresh: {Math.max(500, refreshMs)}ms
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "160px 160px 160px 160px 1fr", gap: 10, alignItems: "center" }}>
        <label style={{ fontSize: 12, opacity: 0.8 }}>Direction</label>
        <select
          value={direction}
          onChange={(e) => setDirection(e.target.value as Direction)}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        >
          <option value="up">Top Gainers</option>
          <option value="down">Top Losers</option>
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Limit</label>
        <input
          type="number"
          value={limit}
          onChange={(e) => setLimit(Number(e.target.value) || DEFAULT_LIMIT)}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        />

        <label style={{ fontSize: 12, opacity: 0.8 }}>Min Quote Vol</label>
        <input
          type="number"
          value={minVol}
          onChange={(e) => setMinVol(Number(e.target.value) || DEFAULT_MIN_VOL)}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        />

        <label style={{ fontSize: 12, opacity: 0.8 }}>Suffix</label>
        <input
          value={suffix}
          onChange={(e) => setSuffix(e.target.value)}
          placeholder="USDT"
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        />

        <label style={{ fontSize: 12, opacity: 0.8 }}>Refresh (ms)</label>
        <input
          type="number"
          value={refreshMs}
          onChange={(e) => setRefreshMs(Math.max(500, Number(e.target.value) || 2000))}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        />

        <div style={{ display: "flex", gap: 10 }}>
          <button
            onClick={() => setPaused((p) => !p)}
            style={{ padding: "8px 12px", borderRadius: 8, background: paused ? "#1f6feb" : "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
          >
            {paused ? "Resume" : "Pause"}
          </button>
          <button
            onClick={() => setRows([])}
            style={{ padding: "8px 12px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
          >
            Clear
          </button>
        </div>
      </div>

      {err ? <div style={{ fontSize: 12, color: "#ff5c7a" }}>Error: {err}</div> : null}
      {lastUpdated ? <div style={{ fontSize: 12, opacity: 0.7 }}>Updated: {lastUpdated}</div> : null}

      <div style={{ padding: 12, border: "1px solid #222", borderRadius: 8, background: "#10151e" }}>
        {rows.length === 0 ? (
          <div style={{ fontSize: 12, opacity: 0.7 }}>No data yet.</div>
        ) : (
          <table style={{ width: "100%", fontSize: 12 }}>
            <thead>
              <tr style={{ textAlign: "left", opacity: 0.7 }}>
                <th>symbol</th>
                <th>pct</th>
                <th>last</th>
                <th>open</th>
                <th>high</th>
                <th>low</th>
                <th>quote vol</th>
                <th>updated</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.symbol}-${r.timestamp}`}>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0", fontWeight: 700 }}>{r.symbol}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0", color: r.pct_change >= 0 ? "#32d583" : "#ff5c7a" }}>
                    {r.pct_change.toFixed(2)}%
                  </td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.close, 6)}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.open, 6)}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.high, 6)}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.low, 6)}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.quote_volume, 2)}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0", opacity: 0.8 }}>{r.timestamp}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
