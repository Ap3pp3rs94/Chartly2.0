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

type ProfileMeta = {
  id: string;
  name?: string;
  version?: string;
  content?: string;
};

const ACTIVE_STREAM_PROFILE = "crypto-watchlist";
const DEFAULT_PROFILE_VERSION = "1.0";

const DEFAULT_SYMBOLS = [
  "BTCUSDT",
  "ETHUSDT",
  "SOLUSDT",
  "XRPUSDT",
  "BNBUSDT",
  "ADAUSDT",
  "AVAXUSDT",
  "DOGEUSDT",
  "LINKUSDT",
  "MATICUSDT"
];

const COLUMN_DEFS = [
  { key: "symbol", label: "Symbol" },
  { key: "pct_change", label: "% Change" },
  { key: "price", label: "Last" },
  { key: "open", label: "Open" },
  { key: "high", label: "High" },
  { key: "low", label: "Low" },
  { key: "volume", label: "Volume" },
  { key: "quote_volume", label: "Quote Vol" },
  { key: "updated", label: "Updated" }
] as const;

const DEFAULT_COLUMNS = COLUMN_DEFS.map((c) => c.key);

const KEY_API = "chartly_api_key";
const SYMBOL_PROFILE_PREFIX = "crypto-";

function fmtNum(v: number, digits = 2) {
  if (!Number.isFinite(v)) return "-";
  const abs = Math.abs(v);
  if (abs >= 1_000_000_000) return `${(v / 1_000_000_000).toFixed(2)}B`;
  if (abs >= 1_000_000) return `${(v / 1_000_000).toFixed(2)}M`;
  if (abs >= 1_000) return `${(v / 1_000).toFixed(2)}K`;
  return v.toFixed(digits);
}

function parseCryptoProfile(content: string): { symbols: string[]; columns: string[] } {
  const lines = content.split(/\r?\n/);
  let inCrypto = false;
  let cryptoIndent = 0;
  let symbols: string[] = [];
  let columns: string[] = [];

  const parseList = (startIdx: number, baseIndent: number) => {
    const out: string[] = [];
    for (let i = startIdx; i < lines.length; i += 1) {
      const line = lines[i];
      if (!line.trim()) continue;
      const indent = line.match(/^(\s*)/)?.[1]?.length ?? 0;
      if (indent <= baseIndent) break;
      const m = line.match(/^\s*-\s*([A-Za-z0-9._-]+)\s*$/);
      if (m) out.push(m[1]);
    }
    return out;
  };

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    if (!inCrypto) {
      const m = line.match(/^(\s*)crypto:\s*$/);
      if (m) {
        inCrypto = true;
        cryptoIndent = m[1].length;
      }
      continue;
    }

    const indent = line.match(/^(\s*)/)?.[1]?.length ?? 0;
    if (indent <= cryptoIndent) break;

    if (line.match(/^\s*symbols:\s*$/)) {
      symbols = parseList(i + 1, indent);
    }
    if (line.match(/^\s*columns:\s*$/)) {
      columns = parseList(i + 1, indent);
    }
  }

  return { symbols, columns };
}

function buildProfileYAML(id: string, name: string, version: string, symbols: string[], columns: string[]) {
  const sym = symbols.map((s) => `    - ${s}`).join("\n");
  const cols = columns.map((c) => `    - ${c}`).join("\n");
  return [
    `id: ${id}`,
    `name: ${name}`,
    `version: "${version}"`,
    ``,
    `crypto:`,
    `  symbols:`,
    sym || "    - BTCUSDT",
    `  columns:`,
    cols || "    - symbol",
    ``
  ].join("\n");
}

function symbolProfileId(symbol: string) {
  return `${SYMBOL_PROFILE_PREFIX}${symbol.trim().toLowerCase()}`;
}

function buildSymbolProfileYAML(id: string, symbol: string, columns: string[]) {
  return buildProfileYAML(id, `Crypto ${symbol}`, DEFAULT_PROFILE_VERSION, [symbol], columns);
}

export default function Crypto() {
  const [rows, setRows] = useState<CryptoRow[]>([]);
  const [direction, setDirection] = useState<Direction>("up");
  const [minVol, setMinVol] = useState<number>(0);
  const [refreshMs, setRefreshMs] = useState<number>(2000);
  const [paused, setPaused] = useState<boolean>(false);
  const [err, setErr] = useState<string>("");
  const [lastUpdated, setLastUpdated] = useState<string>("");

  const [availableSymbols, setAvailableSymbols] = useState<string[]>([]);
  const [selectedSymbols, setSelectedSymbols] = useState<string[]>(DEFAULT_SYMBOLS);
  const [columns, setColumns] = useState<string[]>(DEFAULT_COLUMNS);
  const [search, setSearch] = useState<string>("");
  const [saveStatus, setSaveStatus] = useState<string>("");
  const [saving, setSaving] = useState<boolean>(false);
  const [profiles, setProfiles] = useState<ProfileMeta[]>([]);
  const [activeProfileId, setActiveProfileId] = useState<string>(ACTIVE_STREAM_PROFILE);
  const [activeProfileName, setActiveProfileName] = useState<string>("Crypto Watchlist");
  const [activeProfileVersion, setActiveProfileVersion] = useState<string>(DEFAULT_PROFILE_VERSION);
  const [newProfileId, setNewProfileId] = useState<string>("");
  const [newProfileName, setNewProfileName] = useState<string>("");

  const selectedSet = useMemo(() => new Set(selectedSymbols), [selectedSymbols]);
  const visibleColumns = useMemo(() => new Set(columns), [columns]);

  const params = useMemo(() => {
    if (selectedSymbols.length === 0) return "";
    const p = new URLSearchParams();
    p.set("symbols", selectedSymbols.join(","));
    p.set("limit", String(Math.min(200, Math.max(1, selectedSymbols.length))));
    p.set("direction", direction);
    if (Number.isFinite(minVol) && minVol > 0) p.set("min_vol", String(minVol));
    return p.toString();
  }, [direction, minVol, selectedSymbols]);

  useEffect(() => {
    let alive = true;
    let timer: any;

    const tick = async () => {
      if (!alive || paused || !params) return;
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

  useEffect(() => {
    let alive = true;
    const run = async () => {
      try {
        const res = await fetch("/api/crypto/symbols");
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as { symbols?: string[] };
        if (alive && Array.isArray(data?.symbols)) {
          setAvailableSymbols(data.symbols);
        }
      } catch (e: any) {
        if (alive) setErr(String(e?.message ?? e));
      }
    };
    run();
    return () => {
      alive = false;
    };
  }, []);

  const refreshProfiles = async (aliveRef?: { current: boolean }) => {
    try {
      const res = await fetch(`/api/profiles`);
      if (!res.ok) return;
      const list = (await res.json()) as ProfileMeta[];
      if (aliveRef && !aliveRef.current) return;
      if (!Array.isArray(list)) return;
      const cryptoProfiles = list.filter((p) => typeof p.content === "string" && p.content.includes("crypto:"));
      setProfiles(cryptoProfiles);

      const active = cryptoProfiles.find((p) => p.id === activeProfileId) || cryptoProfiles[0];
      if (active) {
        setActiveProfileId(active.id);
        setActiveProfileName(active.name || active.id);
        setActiveProfileVersion(active.version || DEFAULT_PROFILE_VERSION);
        if (active.content) {
          const parsed = parseCryptoProfile(active.content);
          if (parsed.symbols.length > 0) setSelectedSymbols(parsed.symbols);
          if (parsed.columns.length > 0) setColumns(parsed.columns);
        }
      }
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    const alive = { current: true };
    refreshProfiles(alive);
    return () => {
      alive.current = false;
    };
  }, []);

  useEffect(() => {
    const active = profiles.find((p) => p.id === activeProfileId);
    if (!active || !active.content) return;
    const parsed = parseCryptoProfile(active.content);
    if (parsed.symbols.length > 0) setSelectedSymbols(parsed.symbols);
    if (parsed.columns.length > 0) setColumns(parsed.columns);
    setActiveProfileName(active.name || active.id);
    setActiveProfileVersion(active.version || DEFAULT_PROFILE_VERSION);
  }, [activeProfileId, profiles]);

  const filteredSymbols = useMemo(() => {
    const q = search.trim().toUpperCase();
    if (!q) return availableSymbols;
    return availableSymbols.filter((s) => s.includes(q));
  }, [availableSymbols, search]);

  const toggleSymbol = (sym: string) => {
    setSelectedSymbols((prev) => {
      if (prev.includes(sym)) return prev.filter((s) => s !== sym);
      return [...prev, sym];
    });
  };

  const toggleColumn = (col: string) => {
    setColumns((prev) => {
      if (prev.includes(col)) return prev.filter((c) => c !== col);
      return [...prev, col];
    });
  };

  const saveProfile = async (id: string, name: string, version: string) => {
    const apiKey = sessionStorage.getItem(KEY_API) || "";
    if (!apiKey) {
      setSaveStatus("Missing X-API-Key in Settings");
      return;
    }
    setSaving(true);
    setSaveStatus("");
    try {
      const body = {
        id,
        name,
        version,
        content: buildProfileYAML(id, name, version, selectedSymbols, columns)
      };
      const res = await fetch("/api/profiles", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-API-Key": apiKey },
        body: JSON.stringify(body)
      });
      if (!res.ok) {
        const msg = await res.text();
        throw new Error(msg || `HTTP ${res.status}`);
      }
      await ensureSymbolProfiles(apiKey);
      await refreshProfiles();
      setSaveStatus("Saved. Symbol profiles are ready and charts will update automatically.");
    } catch (e: any) {
      setSaveStatus(String(e?.message ?? e));
    } finally {
      setSaving(false);
    }
  };

  const ensureSymbolProfiles = async (apiKey: string) => {
    const existing = new Set(profiles.map((p) => p.id));
    for (const sym of selectedSymbols) {
      const id = symbolProfileId(sym);
      if (existing.has(id)) continue;
      const body = {
        id,
        name: `Crypto ${sym}`,
        version: DEFAULT_PROFILE_VERSION,
        content: buildSymbolProfileYAML(id, sym, columns)
      };
      const res = await fetch("/api/profiles", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-API-Key": apiKey },
        body: JSON.stringify(body)
      });
      if (!res.ok) {
        // non-fatal; keep going
        continue;
      }
    }
  };

  const createProfile = async () => {
    const id = newProfileId.trim();
    const name = newProfileName.trim() || id;
    if (!id) {
      setSaveStatus("Profile ID is required.");
      return;
    }
    await saveProfile(id, name, DEFAULT_PROFILE_VERSION);
    setActiveProfileId(id);
    setNewProfileId("");
    setNewProfileName("");
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Crypto Watchlist</h1>
      <div style={{ fontSize: 12, opacity: 0.7 }}>
        Select symbols and columns. Save writes to the profile so the stream filters server-side.
      </div>
      <div style={{ fontSize: 12, opacity: 0.7 }}>
        Active stream profile: <span style={{ fontFamily: "monospace" }}>{ACTIVE_STREAM_PROFILE}</span>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(4, minmax(160px, 1fr))", gap: 10, alignItems: "center" }}>
        <label style={{ fontSize: 12, opacity: 0.8 }}>Direction</label>
        <select
          value={direction}
          onChange={(e) => setDirection(e.target.value as Direction)}
          style={{ padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff" }}
        >
          <option value="up">Top Gainers</option>
          <option value="down">Top Losers</option>
        </select>

        <label style={{ fontSize: 12, opacity: 0.8 }}>Min Quote Vol</label>
        <input
          type="number"
          value={minVol}
          onChange={(e) => setMinVol(Number(e.target.value) || 0)}
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

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
        <div style={{ border: "1px solid #222", borderRadius: 8, padding: 12, background: "#0f141c" }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
            <div style={{ fontWeight: 600 }}>Symbols</div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>
              Selected: {selectedSymbols.length} / {availableSymbols.length || "?"}
            </div>
          </div>
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search symbols…"
            style={{ width: "100%", padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff", marginBottom: 10 }}
          />
          <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
            <button
              onClick={() => setSelectedSymbols(availableSymbols)}
              style={{ padding: "6px 10px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
            >
              Select All
            </button>
            <button
              onClick={() => setSelectedSymbols([])}
              style={{ padding: "6px 10px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
            >
              Clear
            </button>
          </div>
          <div style={{ maxHeight: 260, overflow: "auto", borderTop: "1px solid #222", paddingTop: 8 }}>
            {filteredSymbols.length === 0 ? (
              <div style={{ fontSize: 12, opacity: 0.7 }}>No symbols found.</div>
            ) : (
              filteredSymbols.map((sym) => (
                <label key={sym} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12, padding: "4px 0" }}>
                  <input type="checkbox" checked={selectedSet.has(sym)} onChange={() => toggleSymbol(sym)} />
                  <span style={{ fontFamily: "monospace" }}>{sym}</span>
                </label>
              ))
            )}
          </div>
        </div>

        <div style={{ border: "1px solid #222", borderRadius: 8, padding: 12, background: "#0f141c" }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 }}>
            <div style={{ fontWeight: 600 }}>Columns</div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>Visible: {columns.length}</div>
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(140px, 1fr))", gap: 6 }}>
            {COLUMN_DEFS.map((c) => (
              <label key={c.key} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12, padding: "4px 0" }}>
                <input type="checkbox" checked={visibleColumns.has(c.key)} onChange={() => toggleColumn(c.key)} />
                <span>{c.label}</span>
              </label>
            ))}
          </div>
          <div style={{ display: "flex", gap: 10, marginTop: 12 }}>
            <button
              onClick={() => saveProfile(activeProfileId, activeProfileName, activeProfileVersion)}
              disabled={saving}
              style={{ padding: "8px 12px", borderRadius: 8, background: "#1f6feb", color: "#fff", border: "1px solid #1f6feb", cursor: "pointer" }}
            >
              {saving ? "Saving..." : "Save Watchlist"}
            </button>
            <button
              onClick={() => setColumns(DEFAULT_COLUMNS)}
              style={{ padding: "8px 12px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
            >
              Reset Columns
            </button>
          </div>
          {saveStatus ? <div style={{ fontSize: 12, marginTop: 8, opacity: 0.8 }}>{saveStatus}</div> : null}
        </div>
      </div>

      <div style={{ border: "1px solid #222", borderRadius: 8, padding: 12, background: "#0f141c" }}>
        <div style={{ fontWeight: 600, marginBottom: 8 }}>Profiles</div>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
          <div>
            <label style={{ fontSize: 12, opacity: 0.8 }}>Existing Profiles</label>
            <select
              value={activeProfileId}
              onChange={(e) => setActiveProfileId(e.target.value)}
              style={{ width: "100%", padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff", marginTop: 6 }}
            >
              {profiles.length === 0 ? <option value={activeProfileId}>{activeProfileId}</option> : null}
              {profiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name || p.id}
                </option>
              ))}
            </select>
            {activeProfileId !== ACTIVE_STREAM_PROFILE ? (
              <div style={{ fontSize: 12, opacity: 0.7, marginTop: 8 }}>
                Note: crypto stream is still filtered by <span style={{ fontFamily: "monospace" }}>{ACTIVE_STREAM_PROFILE}</span>.
              </div>
            ) : null}
          </div>

          <div>
            <label style={{ fontSize: 12, opacity: 0.8 }}>Create New Profile</label>
            <input
              value={newProfileId}
              onChange={(e) => setNewProfileId(e.target.value)}
              placeholder="profile-id (e.g. crypto-watchlist-alt)"
              style={{ width: "100%", padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff", marginTop: 6 }}
            />
            <input
              value={newProfileName}
              onChange={(e) => setNewProfileName(e.target.value)}
              placeholder="Profile name"
              style={{ width: "100%", padding: "8px 10px", borderRadius: 8, border: "1px solid #333", background: "#111", color: "#fff", marginTop: 8 }}
            />
            <div style={{ display: "flex", gap: 10, marginTop: 10 }}>
              <button
                onClick={createProfile}
                disabled={saving}
                style={{ padding: "8px 12px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
              >
                {saving ? "Creating..." : "Create Profile"}
              </button>
            </div>
          </div>
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
                {COLUMN_DEFS.filter((c) => visibleColumns.has(c.key)).map((c) => (
                  <th key={c.key}>{c.label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={`${r.symbol}-${r.timestamp}`}>
                  {visibleColumns.has("symbol") && (
                    <td style={{ borderTop: "1px solid #222", padding: "6px 0", fontWeight: 700 }}>{r.symbol}</td>
                  )}
                  {visibleColumns.has("pct_change") && (
                    <td style={{ borderTop: "1px solid #222", padding: "6px 0", color: r.pct_change >= 0 ? "#32d583" : "#ff5c7a" }}>
                      {r.pct_change.toFixed(2)}%
                    </td>
                  )}
                  {visibleColumns.has("price") && <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.close, 6)}</td>}
                  {visibleColumns.has("open") && <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.open, 6)}</td>}
                  {visibleColumns.has("high") && <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.high, 6)}</td>}
                  {visibleColumns.has("low") && <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.low, 6)}</td>}
                  {visibleColumns.has("volume") && <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.volume, 2)}</td>}
                  {visibleColumns.has("quote_volume") && <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{fmtNum(r.quote_volume, 2)}</td>}
                  {visibleColumns.has("updated") && (
                    <td style={{ borderTop: "1px solid #222", padding: "6px 0", opacity: 0.8 }}>{r.timestamp}</td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
