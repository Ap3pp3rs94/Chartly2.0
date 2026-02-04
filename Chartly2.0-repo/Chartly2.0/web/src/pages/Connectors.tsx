import React, { useEffect, useMemo, useState } from "react";
import ConnectorHealth from "@/components/connectors/ConnectorHealth";
import ConnectorList, { Connector } from "@/components/connectors/ConnectorList";
import ConnectorConfig from "@/components/connectors/ConnectorConfig";

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
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

export default function Connectors() {
  const [selected, setSelected] = useState<string>("");
  const [schema, setSchema] = useState<any>(undefined);
  const [initial, setInitial] = useState<any>(undefined);
  const [error, setError] = useState<string>("");

  const catalogUrl = "/api/gateway/connectors/catalog";

  const selectedId = useMemo(() => safeString(selected), [selected]);

  useEffect(() => {
    let alive = true;

    async function load() {
      if (!selectedId) {
        setSchema(undefined);
        setInitial(undefined);
        setError("");
        return;
      }

      try {
        setError("");
        // These are placeholder endpoints (may not exist yet). Fail gracefully.
        const schemaUrl = `/api/gateway/connectors/${encodeURIComponent(selectedId)}/schema`;
        const cfgUrl = `/api/gateway/connectors/${encodeURIComponent(selectedId)}/config`;

        const [schemaJson, cfgJson] = await Promise.all([
          fetchJSON(schemaUrl, 2500).catch(() => undefined),
          fetchJSON(cfgUrl, 2500).catch(() => undefined),
        ]);

        if (!alive) return;

        setSchema(schemaJson);
        setInitial(cfgJson);
      } catch (e: any) {
        if (!alive) return;
        setError(String(e?.message ?? e));
        setSchema(undefined);
        setInitial(undefined);
      }
    }

    load();
    return () => {
      alive = false;
    };
  }, [selectedId]);

  const saveUrl = selectedId ? `/api/gateway/connectors/${encodeURIComponent(selectedId)}/config` : undefined;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h1 style={{ margin: 0, fontSize: 20 }}>Connectors</h1>
        <div style={{ opacity: 0.75, fontSize: 12 }}>
          DATA ONLY  catalog/schema/config endpoints may not exist yet
        </div>
      </div>

      <ConnectorHealth connectorIds={selectedId ? [selectedId] : []} />

      <div style={{ display: "grid", gridTemplateColumns: "520px 1fr", gap: 12 }}>
        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          <ConnectorList
            fetchUrl={catalogUrl}
            pollMs={15000}
            onSelect={(id) => setSelected(id)}
          />
        </div>

        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          {error ? (
            <div style={{ padding: 10, border: "1px solid #f2c", borderRadius: 8, color: "#900", marginBottom: 10 }}>
              {error}
            </div>
          ) : null}

          {selectedId ? (
            <ConnectorConfig connectorId={selectedId} schema={schema} initial={initial} saveUrl={saveUrl} />
          ) : (
            <div style={{ opacity: 0.75 }}>Select a connector to view/edit its config.</div>
          )}
        </div>
      </div>
    </div>
  );
}
