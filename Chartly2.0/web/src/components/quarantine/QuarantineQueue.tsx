import React, { useEffect, useMemo, useState } from "react";

export type Severity = "low" | "med" | "high";

export type Item = {
  id: string;
  ts: string; // RFC3339
  source: string;
  kind: string;
  severity: Severity;
  reason: string;
  payload?: any;
};

export type QuarantineQueueProps = {
  items?: Item[];
  fetchUrl?: string;
  pollMs?: number;
  onApprove?: (id: string) => void;
  onReject?: (id: string) => void;
};

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
}

function parseTimeMaybe(s: string): number | null {
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

function normalizeItems(list: any[]): Item[] {
  const out: Item[] = [];
  for (const it of list ?? []) {
    if (!it || typeof it !== "object") continue;
    const id = safeString(it.id);
    const ts = safeString(it.ts);
    const source = safeString(it.source);
    const kind = safeString(it.kind);
    const severity = safeString(it.severity) as Severity;
    const reason = safeString(it.reason);
    if (!id || !ts || !source || !kind || !reason) continue;
    if (severity !== "low" && severity !== "med" && severity !== "high") continue;
    out.push({ id, ts, source, kind, severity, reason, payload: it.payload });
  }
  out.sort((a, b) => {
    const ta = parseTimeMaybe(a.ts);
    const tb = parseTimeMaybe(b.ts);
    if (ta !== null && tb !== null && ta !== tb) return ta - tb;
    if (a.ts !== b.ts) return a.ts.localeCompare(b.ts);
    return a.id.localeCompare(b.id);
  });
  return out;
}

async function fetchList(url: string, timeoutMs: number): Promise<any[]> {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, { method: "GET", signal: ac.signal });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const json = await res.json();
    if (Array.isArray(json)) return json;
    if (Array.isArray(json?.items)) return json.items;
    return [];
  } finally {
    clearTimeout(t);
  }
}

export default function QuarantineQueue(props: QuarantineQueueProps) {
  const pollMs = props.pollMs ?? 0;

  const provided = useMemo(() => (props.items ? normalizeItems(props.items as any[]) : null), [props.items]);
  const [remote, setRemote] = useState<Item[] | null>(null);
  const [error, setError] = useState<string>("");

  const [severity, setSeverity] = useState<Severity | "all">("all");
  const [q, setQ] = useState<string>("");
  const [selectedId, setSelectedId] = useState<string>("");

  useEffect(() => {
    let alive = true;

    async function load() {
      if (!props.fetchUrl) return;
      try {
        const list = await fetchList(props.fetchUrl, 3000);
        const norm = normalizeItems(list);
        if (alive) {
          setRemote(norm);
          setError("");
        }
      } catch (e: any) {
        if (alive) setError(String(e?.message ?? e));
      }
    }

    if (!provided && props.fetchUrl) {
      load();
      if (pollMs > 0) {
        const id = setInterval(load, pollMs);
        return () => {
          alive = false;
          clearInterval(id);
        };
      }
    }

    return () => {
      alive = false;
    };
  }, [props.fetchUrl, pollMs, provided]);

  const items = useMemo(() => provided ?? remote ?? [], [provided, remote]);

  const filtered = useMemo(() => {
    const query = safeString(q).toLowerCase();
    return items.filter((it) => {
      if (severity !== "all" && it.severity !== severity) return false;
      if (!query) return true;
      const hay = `${it.id} ${it.source} ${it.kind} ${it.reason}`.toLowerCase();
      return hay.includes(query);
    });
  }, [items, severity, q]);

  const selected = useMemo(() => {
    const id = safeString(selectedId);
    if (!id) return filtered[0] ?? null;
    return filtered.find((x) => x.id === id) ?? filtered[0] ?? null;
  }, [filtered, selectedId]);

  useEffect(() => {
    if (selected) setSelectedId(selected.id);
  }, [selected?.id]);

  return (
    <div style={{ display: "grid", gridTemplateColumns: "360px 1fr", gap: 12 }}>
      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
          <div style={{ fontWeight: 700 }}>Quarantine Queue</div>
          <div style={{ fontSize: 12, opacity: 0.75 }}>{filtered.length} items</div>
        </div>

        {error ? (
          <div style={{ marginTop: 10, padding: 10, border: "1px solid #f2c", borderRadius: 8, color: "#900" }}>
            {error}
          </div>
        ) : null}

        <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
          <select
            value={severity}
            onChange={(e) => setSeverity(e.target.value as any)}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc" }}
          >
            <option value="all">all</option>
            <option value="low">low</option>
            <option value="med">med</option>
            <option value="high">high</option>
          </select>

          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="search"
            style={{ flex: 1, padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc" }}
          />
        </div>

        <div style={{ marginTop: 10, display: "flex", flexDirection: "column", gap: 8, maxHeight: "70vh", overflowY: "auto" }}>
          {filtered.map((it) => {
            const active = selected?.id === it.id;
            return (
              <button
                key={it.id}
                onClick={() => setSelectedId(it.id)}
                style={{
                  textAlign: "left",
                  padding: 10,
                  borderRadius: 8,
                  border: "1px solid #eee",
                  background: active ? "#eee" : "#fff",
                  cursor: "pointer",
                }}
              >
                <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
                  <strong style={{ fontSize: 12 }}>{it.id}</strong>
                  <span style={{ fontSize: 12 }}>{it.severity.toUpperCase()}</span>
                </div>
                <div style={{ fontSize: 12, opacity: 0.75 }}>
                  {it.source}  {it.kind}
                </div>
                <div style={{ fontSize: 12, opacity: 0.85, marginTop: 4 }}>{it.reason}</div>
                <div style={{ fontSize: 11, opacity: 0.7, marginTop: 4 }}>{it.ts}</div>
              </button>
            );
          })}
          {filtered.length === 0 ? <div style={{ opacity: 0.7, padding: 10 }}>No items.</div> : null}
        </div>
      </div>

      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
          <div>
            <div style={{ fontWeight: 700 }}>Details</div>
            {selected ? (
              <div style={{ opacity: 0.75, fontSize: 12 }}>
                {selected.source}  {selected.kind}  {selected.ts}
              </div>
            ) : null}
          </div>

          <div style={{ display: "flex", gap: 8 }}>
            <button
              disabled={!selected || !props.onReject}
              onClick={() => selected && props.onReject?.(selected.id)}
              style={btnDanger()}
            >
              Reject
            </button>
            <button
              disabled={!selected || !props.onApprove}
              onClick={() => selected && props.onApprove?.(selected.id)}
              style={btn()}
            >
              Approve
            </button>
          </div>
        </div>

        {!selected ? (
          <div style={{ marginTop: 12, opacity: 0.7 }}>No selection.</div>
        ) : (
          <div style={{ marginTop: 12, display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ padding: 10, border: "1px solid #eee", borderRadius: 8 }}>
              <strong>Reason</strong>
              <div style={{ marginTop: 6 }}>{selected.reason}</div>
            </div>

            <div style={{ padding: 10, border: "1px solid #eee", borderRadius: 8 }}>
              <strong>Payload</strong>
              <textarea
                readOnly
                value={JSON.stringify(selected.payload ?? null, null, 2)}
                rows={18}
                style={{ width: "100%", marginTop: 8, padding: 8, borderRadius: 8, border: "1px solid #ccc", fontFamily: "monospace" }}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function btn(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" };
}

function btnDanger(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #f2b", background: "#fff", cursor: "pointer", color: "#900" };
}
