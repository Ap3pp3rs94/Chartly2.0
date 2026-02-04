import React, { useMemo, useState } from "react";
import QuarantineQueue, { Item } from "@/components/quarantine/QuarantineQueue";
import RecoveryPanel from "@/components/quarantine/RecoveryPanel";

function sortItems(items: Item[]): Item[] {
  const cp = items.slice();
  cp.sort((a, b) => {
    if (a.ts !== b.ts) return a.ts.localeCompare(b.ts);
    return a.id.localeCompare(b.id);
  });
  return cp;
}

// Deterministic demo items (DATA ONLY).
const demo: Item[] = sortItems([
  {
    id: "q_001",
    ts: "2026-01-01T00:00:00Z",
    source: "import",
    kind: "profile",
    severity: "low",
    reason: "missing optional fields; needs review",
    payload: { profile_key: "connectors/default", content_type: "application/json", meta: { note: "demo" } },
  },
  {
    id: "q_002",
    ts: "2026-01-02T00:00:00Z",
    source: "connector-hub",
    kind: "artifact",
    severity: "med",
    reason: "unrecognized content_type; requires validation",
    payload: { object_key: "artifacts/local/sha256/aa/bb/...", content_type: "application/octet-stream" },
  },
  {
    id: "q_003",
    ts: "2026-01-03T00:00:00Z",
    source: "ingest",
    kind: "event",
    severity: "high",
    reason: "policy flagged field: detail.ssn",
    payload: { tenant_id: "local", event_id: "e123", detail: { ssn: "123-45-6789" } },
  },
]);

export default function Quarantine() {
  const [items, setItems] = useState<Item[]>(demo);
  const [selectedId, setSelectedId] = useState<string>(demo[0]?.id ?? "");

  const selected = useMemo(() => items.find((x) => x.id === selectedId) ?? items[0] ?? null, [items, selectedId]);

  function remove(id: string) {
    setItems((prev) => sortItems(prev.filter((x) => x.id !== id)));
    setSelectedId((prev) => (prev === id ? "" : prev));
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h1 style={{ margin: 0, fontSize: 20 }}>Quarantine</h1>
        <div style={{ opacity: 0.75, fontSize: 12 }}>DATA ONLY  local demo queue + recovery tools</div>
      </div>

      {/* Queue + details is inside QuarantineQueue, but we also want RecoveryPanel */}
      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <QuarantineQueue
          items={items}
          onApprove={(id) => remove(id)}
          onReject={(id) => remove(id)}
        />
      </div>

      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <RecoveryPanel
          item={selected}
          onRecovered={(id) => remove(id)}
        />
      </div>
    </div>
  );
}
