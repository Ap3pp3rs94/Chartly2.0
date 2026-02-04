import React, { useMemo, useState } from "react";

export type DataGridProps = {
  rows: any[];
  columns?: string[];
  maxInferRows?: number;
  pageSize?: number;
  onRowClick?: (row: any) => void;
};

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function stable(v: any): any {
  if (Array.isArray(v)) return v.map(stable);
  if (isObject(v)) {
    const keys = Object.keys(v).sort((a, b) => a.localeCompare(b));
    const out: Record<string, any> = {};
    for (const k of keys) out[k] = stable(v[k]);
    return out;
  }
  return v;
}

function stableStringify(v: any): string {
  try {
    return JSON.stringify(stable(v));
  } catch {
    return String(v);
  }
}

function inferColumns(rows: any[], maxRows: number): string[] {
  const n = Math.min(Math.max(maxRows, 1), rows.length);
  const set = new Set<string>();
  for (let i = 0; i < n; i++) {
    const r = rows[i];
    if (!isObject(r)) continue;
    for (const k of Object.keys(r)) set.add(String(k));
  }
  const cols = Array.from(set);
  cols.sort((a, b) => a.localeCompare(b));
  return cols;
}

export default function DataGrid(props: DataGridProps) {
  const maxInferRows = props.maxInferRows ?? 50;
  const pageSize = props.pageSize ?? 25;

  const rows = props.rows ?? [];

  const columns = useMemo(() => {
    if (props.columns && props.columns.length > 0) {
      const cols = props.columns.map(String);
      cols.sort((a, b) => a.localeCompare(b));
      // dedup
      const out: string[] = [];
      let last = "";
      for (const c of cols) {
        if (c !== last) out.push(c);
        last = c;
      }
      return out;
    }
    return inferColumns(rows, maxInferRows);
  }, [props.columns, rows, maxInferRows]);

  const [page, setPage] = useState<number>(1);

  const totalPages = useMemo(() => {
    if (rows.length === 0) return 1;
    return Math.max(1, Math.ceil(rows.length / pageSize));
  }, [rows.length, pageSize]);

  const pageRows = useMemo(() => {
    const p = Math.min(Math.max(page, 1), totalPages);
    const start = (p - 1) * pageSize;
    return rows.slice(start, start + pageSize);
  }, [rows, page, pageSize, totalPages]);

  return (
    <div style={{ border: "1px solid #ddd", borderRadius: 8, overflow: "hidden" }}>
      <div style={{ display: "flex", justifyContent: "space-between", padding: 10, background: "#fafafa" }}>
        <div style={{ fontWeight: 700 }}>Data</div>
        <div style={{ fontSize: 12, opacity: 0.8 }}>
          {rows.length} rows  page {Math.min(page, totalPages)} / {totalPages}
        </div>
      </div>

      <div style={{ overflowX: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              {columns.map((c) => (
                <th
                  key={c}
                  style={{
                    textAlign: "left",
                    padding: "10px 8px",
                    borderBottom: "1px solid #eee",
                    fontSize: 12,
                    whiteSpace: "nowrap",
                  }}
                >
                  {c}
                </th>
              ))}
            </tr>
          </thead>

          <tbody>
            {pageRows.map((r, idx) => (
              <tr
                key={idx}
                onClick={() => props.onRowClick?.(r)}
                style={{ cursor: props.onRowClick ? "pointer" : "default" }}
              >
                {columns.map((c) => {
                  const v = isObject(r) ? r[c] : undefined;
                  const text =
                    v == null
                      ? ""
                      : typeof v === "string" || typeof v === "number" || typeof v === "boolean"
                      ? String(v)
                      : stableStringify(v);
                  return (
                    <td key={c} style={{ padding: "8px", borderBottom: "1px solid #f3f3f3", fontSize: 12 }}>
                      {text}
                    </td>
                  );
                })}
              </tr>
            ))}
            {pageRows.length === 0 ? (
              <tr>
                <td colSpan={columns.length || 1} style={{ padding: 12, opacity: 0.7 }}>
                  No rows.
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>

      <div style={{ display: "flex", justifyContent: "space-between", padding: 10 }}>
        <div style={{ display: "flex", gap: 8 }}>
          <button
            onClick={() => setPage(1)}
            disabled={page <= 1}
            style={btn()}
          >
            First
          </button>
          <button
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={page <= 1}
            style={btn()}
          >
            Prev
          </button>
          <button
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            style={btn()}
          >
            Next
          </button>
          <button
            onClick={() => setPage(totalPages)}
            disabled={page >= totalPages}
            style={btn()}
          >
            Last
          </button>
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 12 }}>
          <span>Go to</span>
          <input
            type="number"
            value={page}
            min={1}
            max={totalPages}
            onChange={(e) => setPage(Math.min(totalPages, Math.max(1, Number(e.target.value) || 1)))}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: 80 }}
          />
        </div>
      </div>
    </div>
  );
}

function btn(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" };
}
