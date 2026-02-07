import React, { useMemo, useState } from "react";

type ChartType = "line" | "bar" | "heatmap";

export type ChartSpec = {
  id: string;
  type: ChartType;
  title: string;
  query: string;
  params?: Record<string, string>;
  templateId?: string;
};

export type ReportSpec = {
  id: string;
  title: string;
  description?: string;
  charts: ChartSpec[];
};

let idCounter = 0;

function nextId(prefix: string): string {
  idCounter += 1;
  return `${prefix}_${idCounter}`;
}

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
}

function sortedKeys(obj: Record<string, any>): string[] {
  return Object.keys(obj).sort((a, b) => a.localeCompare(b));
}

function normalizeParams(p: any): Record<string, string> | undefined {
  if (!p || typeof p !== "object" || Array.isArray(p)) return undefined;
  const out: Record<string, string> = {};
  for (const k of sortedKeys(p)) {
    const kk = safeString(k);
    if (!kk) continue;
    out[kk] = safeString(p[k]);
  }
  return Object.keys(out).length ? out : undefined;
}

function normalizeSpec(s: any): ReportSpec | null {
  if (!s || typeof s !== "object" || Array.isArray(s)) return null;

  const id = safeString(s.id) || nextId("report");
  const title = safeString(s.title) || "Untitled Report";
  const description = s.description ? safeString(s.description) : undefined;

  const chartsRaw = Array.isArray(s.charts) ? s.charts : [];
  const charts: ChartSpec[] = chartsRaw
    .filter((c: any) => c && typeof c === "object" && !Array.isArray(c))
    .map((c: any) => ({
      id: safeString(c.id) || nextId("chart"),
      type: (safeString(c.type) as ChartType) || "line",
      title: safeString(c.title) || "Untitled Chart",
      query: safeString(c.query) || "",
      params: normalizeParams(c.params),
      templateId: c.templateId ? safeString(c.templateId) : undefined,
    }))
    .filter((c: ChartSpec) => c.type === "line" || c.type === "bar" || c.type === "heatmap");

  charts.sort((a, b) => a.id.localeCompare(b.id));

  return { id, title, description, charts };
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

async function copyToClipboard(text: string) {
  await navigator.clipboard.writeText(text);
}

export default function ReportBuilder() {
  const [spec, setSpec] = useState<ReportSpec>(() => ({
    id: nextId("report"),
    title: "Chartly Report",
    description: "",
    charts: [],
  }));
  const [importText, setImportText] = useState<string>("");
  const [message, setMessage] = useState<string>("");

  const charts = useMemo(() => {
    const cp = spec.charts.slice();
    cp.sort((a, b) => a.id.localeCompare(b.id));
    return cp;
  }, [spec.charts]);

  function updateSpec(patch: Partial<ReportSpec>) {
    setSpec((prev) => ({ ...prev, ...patch }));
  }

  function updateChart(id: string, patch: Partial<ChartSpec>) {
    setSpec((prev) => {
      const next = prev.charts.map((c) => (c.id === id ? { ...c, ...patch } : c));
      next.sort((a, b) => a.id.localeCompare(b.id));
      return { ...prev, charts: next };
    });
  }

  function addChart() {
    const c: ChartSpec = {
      id: nextId("chart"),
      type: "line",
      title: "New Chart",
      query: "",
      params: undefined,
      templateId: undefined,
    };
    setSpec((prev) => {
      const next = prev.charts.concat([c]);
      next.sort((a, b) => a.id.localeCompare(b.id));
      return { ...prev, charts: next };
    });
  }

  function removeChart(id: string) {
    setSpec((prev) => ({ ...prev, charts: prev.charts.filter((c) => c.id !== id) }));
  }

  function exportJSON() {
    const json = JSON.stringify(spec, null, 2);
    download(`${spec.id}.json`, json);
    setMessage("Exported JSON.");
  }

  async function copyJSON() {
    const json = JSON.stringify(spec, null, 2);
    await copyToClipboard(json);
    setMessage("Copied JSON to clipboard.");
  }

  function importJSON() {
    try {
      const parsed = JSON.parse(importText);
      const ns = normalizeSpec(parsed);
      if (!ns) {
        setMessage("Import failed: invalid shape.");
        return;
      }
      setSpec(ns);
      setMessage("Imported report spec.");
    } catch (e: any) {
      setMessage(`Import failed: ${String(e?.message ?? e)}`);
    }
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
        <div>
          <div style={{ fontWeight: 700, fontSize: 18 }}>Report Builder</div>
          <div style={{ opacity: 0.75, fontSize: 12 }}>DATA ONLY  export/import JSON specs</div>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <button onClick={addChart} style={btn()}>
            Add chart
          </button>
          <button onClick={exportJSON} style={btn()}>
            Export JSON
          </button>
          <button onClick={copyJSON} style={btn()}>
            Copy JSON
          </button>
        </div>
      </div>

      {message ? (
        <div style={{ padding: 10, border: "1px solid #ddd", borderRadius: 8, background: "#fafafa" }}>{message}</div>
      ) : null}

      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 12 }}>
        <div style={card()}>
          <div style={{ fontWeight: 700, marginBottom: 8 }}>Report</div>
          <Field label="ID">
            <input value={spec.id} readOnly style={input()} />
          </Field>
          <Field label="Title">
            <input value={spec.title} onChange={(e) => updateSpec({ title: e.target.value })} style={input()} />
          </Field>
          <Field label="Description">
            <textarea
              value={spec.description ?? ""}
              onChange={(e) => updateSpec({ description: e.target.value })}
              rows={4}
              style={{ ...input(), fontFamily: "inherit" }}
            />
          </Field>
        </div>

        <div style={card()}>
          <div style={{ fontWeight: 700, marginBottom: 8 }}>Import</div>
          <textarea
            value={importText}
            onChange={(e) => setImportText(e.target.value)}
            rows={10}
            style={{ ...input(), fontFamily: "monospace" }}
            placeholder="Paste report JSON here"
          />
          <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
            <button onClick={importJSON} style={btn()}>
              Import JSON
            </button>
            <button
              onClick={() => {
                setImportText(JSON.stringify(spec, null, 2));
                setMessage("Loaded current spec into import box.");
              }}
              style={btn()}
            >
              Load current
            </button>
          </div>
        </div>
      </div>

      <div style={card()}>
        <div style={{ fontWeight: 700, marginBottom: 8 }}>Charts ({charts.length})</div>
        {charts.length === 0 ? <div style={{ opacity: 0.7 }}>No charts yet.</div> : null}

        <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
          {charts.map((c) => (
            <div key={c.id} style={{ border: "1px solid #eee", borderRadius: 8, padding: 10 }}>
              <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
                <div style={{ fontWeight: 700 }}>{c.id}</div>
                <button onClick={() => removeChart(c.id)} style={btnDanger()}>
                  Remove
                </button>
              </div>

              <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 10, marginTop: 8 }}>
                <Field label="Title">
                  <input value={c.title} onChange={(e) => updateChart(c.id, { title: e.target.value })} style={input()} />
                </Field>

                <Field label="Type">
                  <select
                    value={c.type}
                    onChange={(e) => updateChart(c.id, { type: e.target.value as ChartType })}
                    style={input()}
                  >
                    <option value="line">line</option>
                    <option value="bar">bar</option>
                    <option value="heatmap">heatmap</option>
                  </select>
                </Field>

                <Field label="Template ID">
                  <input
                    value={c.templateId ?? ""}
                    onChange={(e) => updateChart(c.id, { templateId: e.target.value || undefined })}
                    style={input()}
                    placeholder="optional"
                  />
                </Field>

                <Field label="Query">
                  <input
                    value={c.query}
                    onChange={(e) => updateChart(c.id, { query: e.target.value })}
                    style={input()}
                    placeholder="data query identifier"
                  />
                </Field>

                <Field label="Params (JSON object)">
                  <textarea
                    value={JSON.stringify(c.params ?? {}, null, 2)}
                    onChange={(e) => {
                      try {
                        const p = JSON.parse(e.target.value);
                        updateChart(c.id, { params: normalizeParams(p) });
                      } catch {
                        // ignore until valid
                      }
                    }}
                    rows={6}
                    style={{ ...input(), fontFamily: "monospace" }}
                  />
                </Field>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div style={card()}>
        <div style={{ fontWeight: 700, marginBottom: 8 }}>Spec JSON</div>
        <textarea value={JSON.stringify(spec, null, 2)} readOnly rows={16} style={{ ...input(), fontFamily: "monospace" }} />
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <label style={{ fontSize: 12, opacity: 0.8 }}>{label}</label>
      {children}
    </div>
  );
}

function card(): React.CSSProperties {
  return { border: "1px solid #ddd", borderRadius: 8, padding: 12, background: "#fff" };
}

function input(): React.CSSProperties {
  return { padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: "100%" };
}

function btn(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" };
}

function btnDanger(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #f2b", background: "#fff", cursor: "pointer", color: "#900" };
}
