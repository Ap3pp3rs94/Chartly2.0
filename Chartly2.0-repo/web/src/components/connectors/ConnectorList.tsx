import React, { useEffect, useMemo, useState } from "react";

export type Connector = {
  id: string;
  kind: string;
  display_name: string;
  description?: string;
  capabilities?: string[];
};

export type ConnectorListProps = {
  connectors?: Connector[];
  fetchUrl?: string;
  pollMs?: number;
  onSelect?: (id: string) => void;
};

function safeString(v: unknown): string {
  return String(v ?? "").trim().replaceAll("\u0000", "");
}

function normalizeConnectors(list: Connector[]): Connector[] {
  const out = (list ?? [])
    .filter((c) => c && typeof c.id === "string" && typeof c.kind === "string" && typeof c.display_name === "string")
    .map((c) => ({
      id: safeString(c.id),
      kind: safeString(c.kind),
      display_name: safeString(c.display_name),
      description: c.description ? safeString(c.description) : undefined,
      capabilities: Array.isArray(c.capabilities)
        ? c.capabilities.map((x) => safeString(x)).filter(Boolean).sort((a, b) => a.localeCompare(b))
        : undefined,
    }))
    .filter((c) => c.id && c.kind && c.display_name);

  out.sort((a, b) => a.id.localeCompare(b.id));
  return out;
}

async function fetchJSON(url: string, timeoutMs: number): Promise<any> {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, { method: "GET", signal: ac.signal });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  } finally {
    clearTimeout(t);
  }
}

export default function ConnectorList(props: ConnectorListProps) {
  const pollMs = props.pollMs ?? 0;
  const [remote, setRemote] = useState<Connector[] | null>(null);
  const [error, setError] = useState<string>("");
  const [q, setQ] = useState<string>("");

  const provided = useMemo(() => (props.connectors ? normalizeConnectors(props.connectors) : null), [props.connectors]);

  useEffect(() => {
    let alive = true;

    async function load() {
      if (!props.fetchUrl) return;
      try {
        const json = await fetchJSON(props.fetchUrl, 3000);
        const list = Array.isArray(json?.connectors) ? json.connectors : Array.isArray(json) ? json : [];
        const norm = normalizeConnectors(list);
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

  const list = useMemo(() => {
    const base = provided ?? remote ?? [];
    const query = safeString(q).toLowerCase();
    if (!query) return base;
    return base.filter((c) => {
      const hay = `${c.id} ${c.display_name} ${c.kind}`.toLowerCase();
      return hay.includes(query);
    });
  }, [provided, remote, q]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
        <div style={{ fontWeight: 700 }}>Connectors</div>
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="search by id, name, kind"
          style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", minWidth: 240 }}
        />
        <div style={{ marginLeft: "auto", fontSize: 12, opacity: 0.75 }}>
          {list.length} items
        </div>
      </div>

      {error ? (
        <div style={{ padding: 10, border: "1px solid #f2c", borderRadius: 8, color: "#900" }}>
          {error}
        </div>
      ) : null}

      <div style={{ overflowX: "auto", border: "1px solid #ddd", borderRadius: 8 }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              {["id", "kind", "display_name", "capabilities", "description"].map((h) => (
                <th key={h} style={{ textAlign: "left", padding: "10px 8px", borderBottom: "1px solid #eee", fontSize: 12 }}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {list.map((c) => (
              <tr
                key={c.id}
                onClick={() => props.onSelect?.(c.id)}
                style={{ cursor: props.onSelect ? "pointer" : "default" }}
              >
                <td style={{ padding: "8px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{c.id}</td>
                <td style={{ padding: "8px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{c.kind}</td>
                <td style={{ padding: "8px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{c.display_name}</td>
                <td style={{ padding: "8px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>
                  {(c.capabilities ?? []).join(", ")}
                </td>
                <td style={{ padding: "8px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>{c.description ?? ""}</td>
              </tr>
            ))}
            {list.length === 0 ? (
              <tr>
                <td colSpan={5} style={{ padding: 12, opacity: 0.7 }}>
                  No connectors found.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}
