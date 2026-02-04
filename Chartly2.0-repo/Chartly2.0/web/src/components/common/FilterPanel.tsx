import React, { useMemo } from "react";

export type FilterDef = {
  id: string;
  label: string;
  type: "text" | "select" | "number";
  options?: string[];
};

export type FilterValue = Record<string, string | number>;

export type FilterPanelProps = {
  filters: FilterDef[];
  value: FilterValue;
  onChange: (next: FilterValue) => void;
};

function normStr(s: string): string {
  const cleaned = s.replaceAll("\u0000", "").trim();
  return cleaned.split(/\s+/g).filter(Boolean).join(" ");
}

function toNum(v: unknown): number {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : 0;
}

function safeString(v: unknown): string {
  return String(v ?? "");
}

export default function FilterPanel(props: FilterPanelProps) {
  const defs = useMemo(() => {
    const d = (props.filters ?? [])
      .filter((f) => f && typeof f.id === "string" && typeof f.label === "string" && typeof f.type === "string")
      .map((f) => ({
        id: normStr(f.id),
        label: normStr(f.label),
        type: f.type as FilterDef["type"],
        options: Array.isArray(f.options) ? f.options.map((x) => normStr(String(x))).filter(Boolean).sort((a, b) => a.localeCompare(b)) : undefined,
      }))
      .filter((f) => f.id && f.label && (f.type === "text" || f.type === "select" || f.type === "number"));

    d.sort((a, b) => a.id.localeCompare(b.id));
    return d;
  }, [props.filters]);

  function setValue(id: string, next: string | number) {
    const cur = props.value ?? {};
    const out: FilterValue = { ...cur };

    if (typeof next === "string") out[id] = normStr(next);
    else out[id] = toNum(next);

    props.onChange(out);
  }

  return (
    <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
      <div style={{ fontWeight: 700, marginBottom: 10 }}>Filters</div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 10 }}>
        {defs.map((f) => {
          const v = props.value?.[f.id];

          if (f.type === "select") {
            const opts = f.options ?? [];
            return (
              <Field key={f.id} label={f.label}>
                <select
                  value={typeof v === "string" ? v : ""}
                  onChange={(e) => setValue(f.id, e.target.value)}
                  style={input()}
                >
                  <option value="">(all)</option>
                  {opts.map((o) => (
                    <option key={o} value={o}>
                      {o}
                    </option>
                  ))}
                </select>
              </Field>
            );
          }

          if (f.type === "number") {
            return (
              <Field key={f.id} label={f.label}>
                <input
                  type="number"
                  value={typeof v === "number" ? v : toNum(v)}
                  onChange={(e) => setValue(f.id, toNum(e.target.value))}
                  style={input()}
                />
              </Field>
            );
          }

          return (
            <Field key={f.id} label={f.label}>
              <input
                value={typeof v === "string" ? v : safeString(v)}
                onChange={(e) => setValue(f.id, e.target.value)}
                style={input()}
              />
            </Field>
          );
        })}
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

function input(): React.CSSProperties {
  return { padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: "100%" };
}
