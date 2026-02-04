import React, { useMemo, useState } from "react";
import type { Item } from "./QuarantineQueue";

type Props = {
  item?: Item | null;
  onRecovered?: (id: string, sanitized: any) => void;
};

function normStr(s: string): string {
  const cleaned = s.replaceAll("\u0000", "").trim();
  return cleaned.split(/\s+/g).filter(Boolean).join(" ");
}

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function sanitize(v: any): any {
  if (v == null) return v;
  if (typeof v === "string") return normStr(v);
  if (typeof v === "number" || typeof v === "boolean") return v;
  if (Array.isArray(v)) return v.map(sanitize); // preserve order
  if (isObject(v)) {
    const keys = Object.keys(v).sort((a, b) => a.localeCompare(b));
    const out: Record<string, any> = {};
    for (const k of keys) out[normStr(String(k))] = sanitize(v[k]);
    return out;
  }
  return normStr(String(v));
}

function stableStringify(v: any): string {
  return JSON.stringify(sanitize(v), null, 2);
}

function download(filename: string, text: string) {
  const blob = new Blob([text], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

export default function RecoveryPanel(props: Props) {
  const item = props.item ?? null;

  const originalText = useMemo(() => JSON.stringify(item?.payload ?? null, null, 2), [item?.payload]);
  const [sanitized, setSanitized] = useState<any>(null);
  const sanitizedText = useMemo(() => (sanitized === null ? "" : JSON.stringify(sanitized, null, 2)), [sanitized]);

  function run() {
    setSanitized(sanitize(item?.payload ?? null));
  }

  function exportSanitized() {
    if (!item) return;
    const text = JSON.stringify(sanitize(item.payload ?? null), null, 2);
    download(`${item.id}_sanitized.json`, text);
  }

  if (!item) {
    return <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8, opacity: 0.75 }}>No item selected.</div>;
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12, border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
        <div>
          <div style={{ fontWeight: 700 }}>Recovery Panel</div>
          <div style={{ opacity: 0.75, fontSize: 12 }}>
            id: {item.id}  {item.source}  {item.kind}  severity {item.severity}
          </div>
        </div>

        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={run} style={btn()}>
            Run sanitize
          </button>
          <button onClick={exportSanitized} style={btn()}>
            Export sanitized
          </button>
          <button
            onClick={() => props.onRecovered?.(item.id, sanitize(item.payload ?? null))}
            style={btn()}
          >
            Mark recovered
          </button>
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 12 }}>
        <div style={card()}>
          <strong>Original payload</strong>
          <textarea
            readOnly
            value={originalText}
            rows={18}
            style={textarea()}
          />
        </div>

        <div style={card()}>
          <strong>Sanitized payload</strong>
          <textarea
            readOnly
            value={sanitized ? sanitizedText : "Run sanitize to generate output."}
            rows={18}
            style={textarea()}
          />
        </div>
      </div>

      <details>
        <summary style={{ cursor: "pointer" }}>Stable JSON preview (sanitized)</summary>
        <textarea readOnly value={stableStringify(item.payload ?? null)} rows={10} style={textarea()} />
      </details>
    </div>
  );
}

function card(): React.CSSProperties {
  return { border: "1px solid #eee", borderRadius: 8, padding: 10, background: "#fff" };
}

function textarea(): React.CSSProperties {
  return { width: "100%", marginTop: 8, padding: 8, borderRadius: 8, border: "1px solid #ccc", fontFamily: "monospace" };
}

function btn(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" };
}
