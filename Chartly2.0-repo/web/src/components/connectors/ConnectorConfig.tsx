import React, { useMemo, useState } from "react";

type Props = {
  connectorId: string;
  schema?: any;
  initial?: any;
  saveUrl?: string;
};

type SaveState = "idle" | "saving" | "ok" | "error";

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
}

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function sortedKeys(obj: Record<string, any>): string[] {
  return Object.keys(obj).sort((a, b) => a.localeCompare(b));
}

function deepClone(v: any): any {
  if (Array.isArray(v)) return v.map(deepClone);
  if (isObject(v)) {
    const out: Record<string, any> = {};
    for (const k of sortedKeys(v)) out[k] = deepClone(v[k]);
    return out;
  }
  return v;
}

function validateRequired(schema: any, value: any): string[] {
  if (!schema || !isObject(schema)) return [];
  const req = Array.isArray(schema.required) ? schema.required.map(String) : [];
  if (req.length === 0) return [];
  const v = isObject(value) ? value : {};
  const missing: string[] = [];
  for (const r of req) {
    if (!(r in v)) missing.push(r);
    else if (v[r] === "" || v[r] === null || v[r] === undefined) missing.push(r);
  }
  missing.sort((a, b) => a.localeCompare(b));
  return missing;
}

function schemaType(schema: any): string {
  const t = schema?.type;
  return typeof t === "string" ? t : "";
}

function renderFieldOrder(schema: any): string[] {
  if (!schema || !isObject(schema)) return [];
  const props = isObject(schema.properties) ? schema.properties : {};
  return sortedKeys(props);
}

function coerceValue(t: string, raw: string): any {
  if (t === "number" || t === "integer") {
    const n = Number(raw);
    return Number.isFinite(n) ? n : raw;
  }
  if (t === "boolean") {
    if (raw === "true") return true;
    if (raw === "false") return false;
    return Boolean(raw);
  }
  return raw;
}

async function postJSON(url: string, body: any, timeoutMs: number): Promise<any> {
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), timeoutMs);
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: ac.signal,
    });
    const ct = res.headers.get("content-type") ?? "";
    const isJSON = ct.includes("application/json");
    const payload = isJSON ? await res.json().catch(() => undefined) : await res.text().catch(() => undefined);
    if (!res.ok) throw new Error(`HTTP ${res.status}: ${typeof payload === "string" ? payload : JSON.stringify(payload)}`);
    return payload;
  } finally {
    clearTimeout(timer);
  }
}

export default function ConnectorConfig(props: Props) {
  const connectorId = safeString(props.connectorId);
  const schema = props.schema;

  const [value, setValue] = useState<any>(() => deepClone(props.initial ?? {}));
  const [rawJSON, setRawJSON] = useState<string>(() => JSON.stringify(props.initial ?? {}, null, 2));
  const [mode, setMode] = useState<"form" | "json">(() => (schemaType(schema) === "object" ? "form" : "json"));
  const [saveState, setSaveState] = useState<SaveState>("idle");
  const [message, setMessage] = useState<string>("");

  const fields = useMemo(() => renderFieldOrder(schema), [schema]);
  const missing = useMemo(() => validateRequired(schema, value), [schema, value]);

  function updateField(path: string, next: any) {
    setValue((prev: any) => {
      const root = isObject(prev) ? deepClone(prev) : {};
      root[path] = next;
      return root;
    });
  }

  function syncJSONFromValue(v: any) {
    setRawJSON(JSON.stringify(v ?? {}, null, 2));
  }

  function syncValueFromJSON(txt: string) {
    try {
      const parsed = JSON.parse(txt);
      setValue(parsed);
      setMessage("");
      setSaveState("idle");
      return true;
    } catch (e: any) {
      setMessage(`Invalid JSON: ${String(e?.message ?? e)}`);
      setSaveState("error");
      return false;
    }
  }

  async function onSave() {
    if (!props.saveUrl) return;
    if (missing.length > 0) {
      setSaveState("error");
      setMessage(`Missing required: ${missing.join(", ")}`);
      return;
    }

    setSaveState("saving");
    setMessage("Saving...");
    try {
      const payload = await postJSON(props.saveUrl, { connector_id: connectorId, config: value }, 5000);
      setSaveState("ok");
      setMessage("Saved.");
      syncJSONFromValue(value);
      return payload;
    } catch (e: any) {
      setSaveState("error");
      setMessage(String(e?.message ?? e));
    }
  }

  if (!connectorId) {
    return <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>Missing connectorId</div>;
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 10 }}>
        <div>
          <div style={{ fontWeight: 700 }}>Connector Config</div>
          <div style={{ opacity: 0.75, fontSize: 12 }}>connector_id: {connectorId}</div>
        </div>

        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <button
            onClick={() => {
              const next = mode === "form" ? "json" : "form";
              setMode(next);
              if (next === "json") syncJSONFromValue(value);
              if (next === "form") syncValueFromJSON(rawJSON);
            }}
            style={{ padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff" }}
          >
            {mode === "form" ? "JSON editor" : "Form editor"}
          </button>

          {props.saveUrl ? (
            <button
              onClick={onSave}
              disabled={saveState === "saving"}
              style={{ padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff" }}
            >
              {saveState === "saving" ? "Saving..." : "Save"}
            </button>
          ) : null}
        </div>
      </div>

      {missing.length > 0 ? (
        <div style={{ padding: 10, border: "1px solid #f5c2c7", borderRadius: 8, color: "#842029", background: "#f8d7da" }}>
          Missing required fields: {missing.join(", ")}
        </div>
      ) : null}

      {message ? (
        <div
          style={{
            padding: 10,
            border: "1px solid #ddd",
            borderRadius: 8,
            color: saveState === "error" ? "#900" : "#111",
            background: saveState === "ok" ? "#e6ffed" : saveState === "error" ? "#fff0f0" : "#fafafa",
          }}
        >
          {message}
        </div>
      ) : null}

      {mode === "form" && schemaType(schema) === "object" ? (
        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          {fields.length === 0 ? (
            <div style={{ opacity: 0.75 }}>Schema has no properties. Switch to JSON editor.</div>
          ) : (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 10 }}>
              {fields.map((k) => {
                const propsSchema = schema?.properties?.[k] ?? {};
                const t = schemaType(propsSchema);
                const label = safeString(propsSchema?.description) || k;
                const cur = isObject(value) ? value[k] : undefined;

                if (t === "boolean") {
                  const val = cur === true ? "true" : cur === false ? "false" : "";
                  return (
                    <div key={k} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                      <label style={{ fontSize: 12, opacity: 0.8 }}>{label}</label>
                      <select
                        value={val}
                        onChange={(e) => updateField(k, e.target.value === "true")}
                        style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc" }}
                      >
                        <option value="">(unset)</option>
                        <option value="true">true</option>
                        <option value="false">false</option>
                      </select>
                    </div>
                  );
                }

                if (t === "number" || t === "integer") {
                  return (
                    <div key={k} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                      <label style={{ fontSize: 12, opacity: 0.8 }}>{label}</label>
                      <input
                        value={cur ?? ""}
                        onChange={(e) => updateField(k, coerceValue(t, e.target.value))}
                        style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc" }}
                      />
                    </div>
                  );
                }

                if (t === "string") {
                  return (
                    <div key={k} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                      <label style={{ fontSize: 12, opacity: 0.8 }}>{label}</label>
                      <input
                        value={cur ?? ""}
                        onChange={(e) => updateField(k, e.target.value)}
                        style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc" }}
                      />
                    </div>
                  );
                }

                return (
                  <div key={k} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                    <label style={{ fontSize: 12, opacity: 0.8 }}>{label}</label>
                    <textarea
                      value={JSON.stringify(cur ?? null, null, 2)}
                      onChange={(e) => {
                        try {
                          updateField(k, JSON.parse(e.target.value));
                        } catch {
                          // ignore until valid
                        }
                      }}
                      rows={6}
                      style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", fontFamily: "monospace" }}
                    />
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          <textarea
            value={rawJSON}
            onChange={(e) => {
              setRawJSON(e.target.value);
              syncValueFromJSON(e.target.value);
            }}
            rows={18}
            style={{ width: "100%", padding: "8px", borderRadius: 8, border: "1px solid #ccc", fontFamily: "monospace" }}
          />
        </div>
      )}
    </div>
  );
}
