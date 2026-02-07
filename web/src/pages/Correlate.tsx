import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

type Profile = { id: string; name?: string };

async function fetchJson(url: string): Promise<any> {
  const res = await fetch(url);
  if (!res.ok) return null;
  return await res.json();
}

export default function Correlate() {
  const nav = useNavigate();
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [selected, setSelected] = useState<string[]>([]);
  const [msg, setMsg] = useState("");

  useEffect(() => {
    fetchJson("/api/profiles")
      .then((data) => {
        const list = Array.isArray(data) ? data : data?.profiles ?? [];
        const normalized = list.map((p: any) => ({ id: p.id, name: p.name || p.id }));
        normalized.sort((a: Profile, b: Profile) => a.id.localeCompare(b.id));
        setProfiles(normalized);
      })
      .catch(() => setProfiles([]));
  }, []);

  function toggle(id: string) {
    const next = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
    next.sort();
    setSelected(next);
  }

  async function createReport() {
    setMsg("");
    if (!selected.length) {
      setMsg("Select at least one profile.");
      return;
    }
    try {
      const res = await fetch("/api/reports", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ profiles: selected, mode: "auto" })
      });
      const data = await res.json();
      if (res.ok && data?.id) {
        nav(`/charts?report=${encodeURIComponent(data.id)}`);
        return;
      }
      setMsg("Unable to create report.");
    } catch {
      setMsg("Unable to create report.");
    }
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20, letterSpacing: 0.4 }}>Profiles</h1>
      <div style={{ fontSize: 12, opacity: 0.7 }}>Select profiles to create a report.</div>

      <div style={{ display: "grid", gap: 8 }}>
        {profiles.map((p) => (
          <label key={p.id} style={{ display: "grid", gridTemplateColumns: "24px 1fr", gap: 10, alignItems: "center", padding: 8, border: "1px solid #1f2228", borderRadius: 4, background: "#0f1115" }}>
            <input type="checkbox" checked={selected.includes(p.id)} onChange={() => toggle(p.id)} />
            <div>
              <div style={{ fontWeight: 600 }}>{p.name || p.id}</div>
              <div style={{ fontSize: 11, opacity: 0.6 }}>{p.id}</div>
            </div>
          </label>
        ))}
      </div>

      <div style={{ display: "flex", gap: 8 }}>
        <button onClick={createReport} style={{ padding: "8px 12px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}>
          Create Report
        </button>
      </div>

      {msg ? <div style={{ fontSize: 12, opacity: 0.7 }}>{msg}</div> : null}
    </div>
  );
}
