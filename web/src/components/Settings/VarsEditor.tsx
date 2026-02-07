import React, { useMemo, useState } from "react";
import { connectionTemplates, normalizeVarName } from "@/lib/connections";
import { getVars, setVars, VarsMap } from "@/lib/storage";

export default function VarsEditor() {
  const [vars, setVarsState] = useState<VarsMap>(() => getVars());
  const [show, setShow] = useState<Record<string, boolean>>({});

  const entries = useMemo(() => {
    const names = new Set<string>();
    for (const tpl of connectionTemplates) for (const v of tpl.vars) names.add(normalizeVarName(v.name));
    for (const k of Object.keys(vars)) names.add(normalizeVarName(k));
    return Array.from(names);
  }, [vars]);

  function update(name: string, value: string) {
    const next = { ...vars, [name]: value };
    setVarsState(next);
  }

  function save(name: string) {
    const next = { ...vars };
    setVars(next);
  }

  function clear(name: string) {
    const next = { ...vars };
    delete next[name];
    setVarsState(next);
    setVars(next);
  }

  return (
    <div style={{ padding: 14, borderRadius: 6, border: "1px solid #1f2228", background: "#0f1115" }}>
      <div style={{ fontSize: 16, fontWeight: 800, marginBottom: 6 }}>🔑 API Keys / Connection Variables</div>
      <div style={{ fontSize: 12, opacity: 0.7, marginBottom: 12 }}>
        Keys never leave your machine except as request headers to your own gateway.
      </div>

      {entries.map((name) => {
        const val = vars[name] || "";
        const isShown = !!show[name];
        return (
          <div key={name} style={{ display: "grid", gridTemplateColumns: "200px 1fr 100px 80px 80px", gap: 8, alignItems: "center", marginBottom: 8 }}>
            <div style={{ fontSize: 12, opacity: 0.85 }}>{name}</div>
            <input
              type={isShown ? "text" : "password"}
              placeholder="PASTE API KEY HERE (stored only in this browser)"
              value={val}
              onChange={(e) => update(name, e.target.value)}
              style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" }}
            />
            <button
              onClick={() => setShow((s) => ({ ...s, [name]: !s[name] }))}
              style={{ padding: "6px 8px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6", cursor: "pointer" }}
            >
              {isShown ? "Hide" : "Show"}
            </button>
            <button
              onClick={() => save(name)}
              style={{ padding: "6px 8px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6", cursor: "pointer" }}
            >
              Save
            </button>
            <button
              onClick={() => clear(name)}
              style={{ padding: "6px 8px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6", cursor: "pointer" }}
            >
              Clear
            </button>
          </div>
        );
      })}
    </div>
  );
}
