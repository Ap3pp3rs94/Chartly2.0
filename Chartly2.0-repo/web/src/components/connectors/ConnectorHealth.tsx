import React, { useEffect, useMemo, useState } from "react";

type HealthState = "unknown" | "ok" | "error";

type Item = {
  id: string;
  label: string;
  url: string;
  state: HealthState;
  lastCheckedAt?: string;
  error?: string;
};

function nowIso(): string {
  return new Date().toISOString();
}

async function check(url: string, timeoutMs: number): Promise<{ ok: boolean; status: number; detail?: any }> {
  const ac = new AbortController();
  const t = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, { method: "GET", signal: ac.signal });
    const ct = res.headers.get("content-type") ?? "";
    const isJSON = ct.includes("application/json");
    const detail = isJSON ? await res.json().catch(() => undefined) : await res.text().catch(() => undefined);
    return { ok: res.ok, status: res.status, detail };
  } catch (e: any) {
    return { ok: false, status: 0, detail: { error: e?.name === "AbortError" ? "timeout" : String(e) } };
  } finally {
    clearTimeout(t);
  }
}

export type ConnectorHealthProps = {
  connectorIds?: string[];
};

export default function ConnectorHealth(props: ConnectorHealthProps) {
  const ids = useMemo(() => {
    const arr = (props.connectorIds ?? []).map((x) => String(x ?? "").trim()).filter(Boolean);
    arr.sort((a, b) => a.localeCompare(b));
    const out: string[] = [];
    let last = "";
    for (const s of arr) {
      if (s !== last) out.push(s);
      last = s;
    }
    return out;
  }, [props.connectorIds]);

  const [hub, setHub] = useState<Item>({
    id: "hub",
    label: "Connector Hub",
    url: "/api/gateway/connectors/health",
    state: "unknown",
  });

  const [items, setItems] = useState<Item[]>([]);

  useEffect(() => {
    let alive = true;

    async function load() {
      const r = await check("/api/gateway/connectors/health", 2500);
      let hubNext: Item = {
        id: "hub",
        label: "Connector Hub",
        url: "/api/gateway/connectors/health",
        state: r.ok ? "ok" : "error",
        lastCheckedAt: nowIso(),
        error: r.ok ? undefined : `HTTP ${r.status}`,
      };

      if (!r.ok && r.status === 404) {
        const r2 = await check("/api/gateway/health", 2500);
        hubNext = {
          ...hubNext,
          url: "/api/gateway/health",
          state: r2.ok ? "ok" : "error",
          error: r2.ok ? undefined : `HTTP ${r2.status}`,
        };
      }

      const nextItems: Item[] = [];
      for (const id of ids) {
        const url = `/api/gateway/connectors/${encodeURIComponent(id)}/health`;
        const rr = await check(url, 2500);
        nextItems.push({
          id,
          label: id,
          url,
          state: rr.ok ? "ok" : "error",
          lastCheckedAt: nowIso(),
          error: rr.ok ? undefined : `HTTP ${rr.status}`,
        });
      }

      if (alive) {
        setHub(hubNext);
        setItems(nextItems);
      }
    }

    load();
    const timer = setInterval(load, 10000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [ids]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ fontWeight: 700 }}>Connector Health</div>

      <Card item={hub} />

      {items.length > 0 ? (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 10 }}>
          {items.map((it) => (
            <Card key={it.id} item={it} />
          ))}
        </div>
      ) : (
        <div style={{ opacity: 0.7, fontSize: 12 }}>No connector IDs provided.</div>
      )}
    </div>
  );
}

function Card({ item }: { item: Item }) {
  return (
    <div style={{ padding: 10, border: "1px solid #ddd", borderRadius: 8 }}>
      <div style={{ display: "flex", justifyContent: "space-between" }}>
        <strong>{item.label}</strong>
        <span>{item.state === "ok" ? "OK" : item.state === "error" ? "ERR" : "..."}</span>
      </div>
      <div style={{ opacity: 0.7, fontSize: 12 }}>{item.url}</div>
      <div style={{ opacity: 0.7, fontSize: 12 }}>
        {item.lastCheckedAt ? `checked ${item.lastCheckedAt}` : "not checked yet"}
      </div>
      {item.error ? <div style={{ color: "#900", fontSize: 12 }}>{item.error}</div> : null}
    </div>
  );
}
