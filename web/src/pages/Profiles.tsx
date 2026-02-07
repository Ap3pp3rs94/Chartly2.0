import React, { useEffect, useMemo, useState } from "react";
import { getAllProfiles } from "@/lib/profile_virtual";
import { loadWorkspace, setSelectedProfiles } from "@/lib/workspace";
import { useNavigate } from "react-router-dom";
import { getVirtualProfiles, setVirtualProfiles } from "@/lib/storage";

export default function Profiles() {
  const nav = useNavigate();
  const [profiles, setProfiles] = useState<any[]>([]);
  const [selected, setSelected] = useState<string[]>(() => loadWorkspace().selectedProfiles);
  const [pins, setPins] = useState<string[]>(() => {
    try {
      const raw = localStorage.getItem("chartly.pins.v1");
      return raw ? JSON.parse(raw) : [];
    } catch {
      return [];
    }
  });

  useEffect(() => {
    getAllProfiles().then(setProfiles);
  }, []);

  useEffect(() => {
    setSelectedProfiles(selected);
  }, [selected]);

  function toggleSelect(id: string) {
    const next = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
    setSelected(next.sort());
  }

  function togglePin(id: string) {
    const next = pins.includes(id) ? pins.filter((x) => x !== id) : [...pins, id];
    setPins(next);
    localStorage.setItem("chartly.pins.v1", JSON.stringify(next));
  }

  function removeVirtual(id: string) {
    const list = getVirtualProfiles().filter((p) => p.id !== id);
    setVirtualProfiles(list);
    getAllProfiles().then(setProfiles);
  }

  const sorted = useMemo(() => {
    return profiles.slice().sort((a, b) => a.id.localeCompare(b.id));
  }, [profiles]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Profiles</h1>

      <div style={{ display: "flex", gap: 8 }}>
        <button onClick={() => nav("/correlate")} style={{ padding: "8px 12px", borderRadius: 8, border: "1px solid #1f2a37", background: "#1f6feb", color: "#fff" }}>Correlate</button>
        <button onClick={() => nav("/charts")} style={{ padding: "8px 12px", borderRadius: 8, border: "1px solid #1f2a37", background: "#222", color: "#fff" }}>Charts</button>
      </div>

      <div style={{ display: "grid", gap: 8 }}>
        {sorted.map((p) => (
          <div key={p.id} style={{ padding: 12, borderRadius: 10, border: "1px solid #1f2a37", background: "#0f141c", display: "grid", gridTemplateColumns: "24px 24px 1fr 80px 120px", gap: 10, alignItems: "center" }}>
            <input type="checkbox" checked={selected.includes(p.id)} onChange={() => toggleSelect(p.id)} />
            <button onClick={() => togglePin(p.id)} style={{ border: "none", background: "transparent", color: pins.includes(p.id) ? "#ffd166" : "#666" }}>★</button>
            <div>
              <div style={{ fontWeight: 700 }}>{p.name || p.id}</div>
              <div style={{ fontSize: 12, opacity: 0.7 }}>{p.id}</div>
            </div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>{p._virtual ? "imported" : "real"}</div>
            <div style={{ display: "flex", gap: 6 }}>
              <button onClick={() => nav(`/charts?profiles=${encodeURIComponent(p.id)}`)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#111827", color: "#e5e7eb" }}>Open</button>
              {p._virtual ? (
                <button onClick={() => removeVirtual(p.id)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#222", color: "#fff" }}>Remove</button>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
