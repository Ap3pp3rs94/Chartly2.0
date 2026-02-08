import React, { useEffect, useMemo, useState } from "react";
import { getSettings } from "@/lib/storage";
import { getVars } from "@/lib/storage";

type CryptoRow = {
  symbol: string;
  pct_change?: number;
  price?: number;
  volume?: number;
  quote_volume?: number;
  high?: number;
  low?: number;
  open?: number;
  updated?: string;
};

type Profile = { id: string; name?: string; content?: string };

const columnDefs = [
  { id: "symbol", label: "Symbol" },
  { id: "pct_change", label: "% Change" },
  { id: "price", label: "Last" },
  { id: "open", label: "Open" },
  { id: "high", label: "High" },
  { id: "low", label: "Low" },
  { id: "volume", label: "Volume" },
  { id: "quote_volume", label: "Quote Vol" },
  { id: "updated", label: "Updated" },
] as const;

type ColumnId = typeof columnDefs[number]["id"];

function nowIso() {
  return new Date().toISOString();
}

function getRegistryKey() {
  const vars = getVars();
  return (
    vars["REGISTRY_API_KEY"] ||
    vars["X_API_KEY"] ||
    vars["API_KEY"] ||
    ""
  );
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

function parseList(content: string, key: string): string[] {
  const lines = content.split(/\r?\n/);
  const out: string[] = [];
  let inSection = false;
  for (const line of lines) {
    const trimmed = line.trim();
    if (!inSection) {
      if (trimmed === `${key}:`) {
        inSection = true;
      }
      continue;
    }
    if (!line.startsWith(" ")) break;
    if (trimmed.startsWith("- ")) {
      const val = trimmed.slice(2).trim();
      if (val) out.push(val);
    }
  }
  return out;
}

function buildCryptoProfileYaml(id: string, name: string, symbols: string[], columns: ColumnId[]) {
  const symLines = symbols.map((s) => `    - ${s}`).join("\n");
  const colLines = columns.map((c) => `    - ${c}`).join("\n");
  return `id: ${id}
name: ${name}
version: "1.0"

crypto:
  symbols:
${symLines || "    - BTCUSDT"}
  columns:
${colLines || "    - symbol"}
`;
}

export default function Crypto() {
  const [symbols, setSymbols] = useState<string[]>([]);
  const [rows, setRows] = useState<CryptoRow[]>([]);
  const [query, setQuery] = useState("");
  const [selectedSymbols, setSelectedSymbols] = useState<Set<string>>(new Set());
  const [visibleCols, setVisibleCols] = useState<Set<ColumnId>>(
    new Set(columnDefs.map((c) => c.id))
  );
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [activeProfile, setActiveProfile] = useState<string>("crypto-watchlist");
  const [profileName, setProfileName] = useState<string>("Crypto Watchlist");
  const [direction, setDirection] = useState<string>("gainers");
  const [limit, setLimit] = useState<number>(25);
  const [suffix, setSuffix] = useState<string>("USDT");
  const [minQuoteVol, setMinQuoteVol] = useState<number>(0);
  const settings = getSettings();
  const [refreshMs, setRefreshMs] = useState<number>(Math.max(500, Number(settings.refreshMs) || 2000));
  const [status, setStatus] = useState<string>("");
  const [lastUpdated, setLastUpdated] = useState<string>(nowIso());

  useEffect(() => {
    fetchJson("/api/crypto/symbols").then((data) => {
      const list = Array.isArray(data)
        ? data
        : Array.isArray(data?.symbols)
        ? data.symbols
        : Array.isArray(data?.data)
        ? data.data
        : [];
      if (!list.length) {
        return;
      }
      const cleaned = list
        .map((v: any) => (typeof v === "string" ? v : v?.symbol || v?.s))
        .filter((v: any) => typeof v === "string" && v.length > 0);
      if (!cleaned.length) {
        return;
      }
      cleaned.sort();
      setSymbols(cleaned);
    });
  }, []);

  useEffect(() => {
    fetchJson("/api/profiles").then((data) => {
      const list = Array.isArray(data) ? data : data?.profiles ?? [];
      const normalized = list.map((p: any) => ({
        id: p.id,
        name: p.name || p.id,
        content: p.content,
      }));
      normalized.sort((a: Profile, b: Profile) => a.id.localeCompare(b.id));
      setProfiles(normalized);
    });
  }, []);

  useEffect(() => {
    const prof = profiles.find((p) => p.id === activeProfile);
    if (!prof || !prof.content) return;
    const symbols = parseList(prof.content, "symbols");
    const cols = parseList(prof.content, "columns") as ColumnId[];
    if (symbols.length) setSelectedSymbols(new Set(symbols));
    if (cols.length) setVisibleCols(new Set(cols));
    if (prof.name) setProfileName(prof.name);
  }, [activeProfile, profiles]);

  useEffect(() => {
    let stopped = false;
    let pollTimer: number | undefined;
    const params = new URLSearchParams();
    params.set("limit", String(limit));
    params.set("direction", direction);
    params.set("suffix", suffix);
    params.set("min_quote_vol", String(minQuoteVol));

    const poll = async () => {
      if (stopped) return;
      const data = await fetchJson(`/api/crypto/top?${params.toString()}`, 9000);
      if (data && Array.isArray(data)) {
        setRows(data);
        setLastUpdated(nowIso());
        setStatus("Live");
      }
      pollTimer = window.setTimeout(poll, refreshMs);
    };

    const es = new EventSource(`/api/crypto/stream?${params.toString()}`);
    es.addEventListener("tickers", (evt) => {
      try {
        const payload = JSON.parse((evt as MessageEvent).data || "{}");
        if (Array.isArray(payload.rows)) {
          setRows(payload.rows);
          setLastUpdated(payload.updated || nowIso());
          setStatus("Live");
        }
      } catch {
        setStatus("Stream error");
      }
    });
    es.addEventListener("error", () => {
      setStatus("Streaming unavailable. Falling back to polling.");
      es.close();
      poll();
    });

    return () => {
      stopped = true;
      if (pollTimer) window.clearTimeout(pollTimer);
      es.close();
    };
  }, [direction, limit, suffix, minQuoteVol, refreshMs]);

  const filteredSymbols = useMemo(() => {
    const q = query.trim().toUpperCase();
    const list = symbols.filter((s) => !q || s.includes(q));
    return list.slice(0, 500);
  }, [symbols, query]);

  const visibleColumns = useMemo(() => {
    return columnDefs.filter((c) => visibleCols.has(c.id));
  }, [visibleCols]);

  const displayRows = useMemo(() => {
    let list = rows.slice();
    if (selectedSymbols.size) {
      list = list.filter((r) => selectedSymbols.has(r.symbol));
    }
    return list;
  }, [rows, selectedSymbols]);

  function toggleSymbol(sym: string) {
    const next = new Set(selectedSymbols);
    if (next.has(sym)) next.delete(sym);
    else next.add(sym);
    setSelectedSymbols(next);
  }

  function toggleColumn(col: ColumnId) {
    const next = new Set(visibleCols);
    if (next.has(col)) next.delete(col);
    else next.add(col);
    setVisibleCols(next);
  }

  async function saveProfile() {
    setStatus("");
    const key = getRegistryKey();
    if (!key) {
      setStatus("Missing REGISTRY_API_KEY (set it in Settings).");
      return;
    }
    const yaml = buildCryptoProfileYaml(
      activeProfile || "crypto-watchlist",
      profileName || "Crypto Watchlist",
      Array.from(selectedSymbols),
      Array.from(visibleCols)
    );
    const exists = profiles.some((p) => p.id === activeProfile);
    const method = exists ? "PUT" : "POST";
    const url = exists ? `/api/profiles/${encodeURIComponent(activeProfile)}` : "/api/profiles";
    const body = {
      id: activeProfile || "crypto-watchlist",
      name: profileName || "Crypto Watchlist",
      version: "1.0",
      content: yaml,
    };
    try {
      const res = await fetch(url, {
        method,
        headers: { "Content-Type": "application/json", "X-API-Key": key },
        body: JSON.stringify(body),
      });
      if (res.ok) {
        setStatus("Saved.");
        const refreshed = await fetchJson("/api/profiles");
        const list = Array.isArray(refreshed) ? refreshed : refreshed?.profiles ?? [];
        setProfiles(list);
        return;
      }
      const err = await res.json().catch(() => ({}));
      setStatus(`Save failed: ${err?.error || res.status}`);
    } catch {
      setStatus("Save failed.");
    }
  }

  return (
    <div style={{ display: "grid", gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <div style={{ fontSize: 18, fontWeight: 800 }}>Crypto Live</div>
        <div style={{ fontSize: 12, opacity: 0.7 }}>Updated {lastUpdated}</div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr 1fr", gap: 10 }}>
        <label style={{ display: "grid", gap: 4 }}>
          <span style={{ fontSize: 12, opacity: 0.7 }}>Direction</span>
          <select value={direction} onChange={(e) => setDirection(e.target.value)} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }}>
            <option value="gainers">Top Gainers</option>
            <option value="losers">Top Losers</option>
          </select>
        </label>
        <label style={{ display: "grid", gap: 4 }}>
          <span style={{ fontSize: 12, opacity: 0.7 }}>Limit</span>
          <input type="number" value={limit} onChange={(e) => setLimit(Math.max(1, Number(e.target.value) || 25))} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }} />
        </label>
        <label style={{ display: "grid", gap: 4 }}>
          <span style={{ fontSize: 12, opacity: 0.7 }}>Suffix</span>
          <input value={suffix} onChange={(e) => setSuffix(e.target.value.toUpperCase())} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }} />
        </label>
        <label style={{ display: "grid", gap: 4 }}>
          <span style={{ fontSize: 12, opacity: 0.7 }}>Min Quote Vol</span>
          <input type="number" value={minQuoteVol} onChange={(e) => setMinQuoteVol(Math.max(0, Number(e.target.value) || 0))} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }} />
        </label>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #1f2228", background: "#0f1115" }}>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 8 }}>
            <div style={{ fontWeight: 700 }}>Symbols</div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Selected {selectedSymbols.size}</div>
          </div>
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search symbols..." style={{ width: "100%", padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6", marginBottom: 8 }} />
          <div style={{ maxHeight: 280, overflow: "auto", border: "1px solid #1f2228", borderRadius: 4, padding: 6, background: "#0b0c10" }}>
            {filteredSymbols.length === 0 ? (
              <div style={{ fontSize: 12, opacity: 0.6 }}>No symbols found.</div>
            ) : (
              filteredSymbols.map((s) => (
                <label key={s} style={{ display: "flex", alignItems: "center", gap: 8, padding: "4px 2px" }}>
                  <input type="checkbox" checked={selectedSymbols.has(s)} onChange={() => toggleSymbol(s)} />
                  <span style={{ fontSize: 12 }}>{s}</span>
                </label>
              ))
            )}
          </div>
        </div>

        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #1f2228", background: "#0f1115" }}>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 8 }}>
            <div style={{ fontWeight: 700 }}>Columns</div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Visible {visibleCols.size}</div>
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6 }}>
            {columnDefs.map((c) => (
              <label key={c.id} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <input type="checkbox" checked={visibleCols.has(c.id)} onChange={() => toggleColumn(c.id)} />
                <span style={{ fontSize: 12 }}>{c.label}</span>
              </label>
            ))}
          </div>
          <div style={{ marginTop: 12, display: "grid", gap: 6 }}>
            <label style={{ fontSize: 12, opacity: 0.7 }}>Profile</label>
            <select value={activeProfile} onChange={(e) => setActiveProfile(e.target.value)} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }}>
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>{p.name || p.id}</option>
              ))}
            </select>
            <input value={activeProfile} onChange={(e) => setActiveProfile(e.target.value)} placeholder="profile-id" style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }} />
            <input value={profileName} onChange={(e) => setProfileName(e.target.value)} placeholder="Profile name" style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }} />
            <button onClick={saveProfile} style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}>
              Save Watchlist
            </button>
            {status ? <div style={{ fontSize: 12, opacity: 0.7 }}>{status}</div> : null}
          </div>
        </div>
      </div>

      <div style={{ border: "1px solid #1f2228", borderRadius: 8, background: "#0f1115", overflow: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
          <thead>
            <tr>
              {visibleColumns.map((c) => (
                <th key={c.id} style={{ textAlign: "left", padding: "8px 10px", borderBottom: "1px solid #1f2228" }}>{c.label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {displayRows.map((r) => (
              <tr key={r.symbol}>
                {visibleColumns.map((c) => (
                  <td key={c.id} style={{ padding: "6px 10px", borderBottom: "1px solid #14161a" }}>
                    {typeof (r as any)[c.id] === "number" ? Number((r as any)[c.id]).toFixed(4) : (r as any)[c.id]}
                  </td>
                ))}
              </tr>
            ))}
            {displayRows.length === 0 ? (
              <tr>
                <td colSpan={visibleColumns.length} style={{ padding: "10px", opacity: 0.6 }}>
                  No data yet.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}
