import React, { useMemo, useState } from "react";
import type { ReportSpec } from "./ReportBuilder";

type Props = {
  report: ReportSpec;
  data?: Record<string, any>;
};

function download(filename: string, text: string) {
  const blob = new Blob([text], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
}

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function stable(value: any): any {
  if (Array.isArray(value)) return value.map(stable);
  if (isObject(value)) {
    const keys = Object.keys(value).sort((a, b) => a.localeCompare(b));
    const out: Record<string, any> = {};
    for (const k of keys) out[k] = stable(value[k]);
    return out;
  }
  return value;
}

function stableStringify(value: any): string {
  return JSON.stringify(stable(value), null, 2);
}

export default function ExportOptions(props: Props) {
  const [msg, setMsg] = useState<string>("");

  const reportJSON = useMemo(() => stableStringify(props.report), [props.report]);
  const snapshotJSON = useMemo(
    () => stableStringify({ report: props.report, data: props.data ?? {} }),
    [props.report, props.data]
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ fontWeight: 700 }}>Export</div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <button
          onClick={() => {
            download(`${props.report.id}.json`, reportJSON);
            setMsg("Exported report JSON.");
          }}
          style={btn()}
        >
          Export Report JSON
        </button>

        <button
          onClick={() => {
            download(`${props.report.id}_snapshot.json`, snapshotJSON);
            setMsg("Exported snapshot JSON.");
          }}
          style={btn()}
        >
          Export Snapshot JSON
        </button>

        <button
          onClick={async () => {
            await copy(reportJSON);
            setMsg("Copied report JSON.");
          }}
          style={btn()}
        >
          Copy Report JSON
        </button>

        <button
          onClick={async () => {
            await copy(snapshotJSON);
            setMsg("Copied snapshot JSON.");
          }}
          style={btn()}
        >
          Copy Snapshot JSON
        </button>
      </div>

      {msg ? <div style={{ padding: 10, border: "1px solid #ddd", borderRadius: 8, background: "#fafafa" }}>{msg}</div> : null}

      <details>
        <summary style={{ cursor: "pointer" }}>Preview JSON</summary>
        <textarea
          readOnly
          value={reportJSON}
          rows={10}
          style={{ width: "100%", marginTop: 8, padding: 8, borderRadius: 8, border: "1px solid #ccc", fontFamily: "monospace" }}
        />
      </details>
    </div>
  );
}

function btn(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" };
}
