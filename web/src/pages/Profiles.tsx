import React, { useEffect, useMemo, useState } from "react";
import { getAllProfiles } from "@/lib/profile_virtual";
import { loadWorkspace, setSelectedProfiles } from "@/lib/workspace";
import { useNavigate } from "react-router-dom";
import { getVirtualProfiles, setVirtualProfiles, getVars } from "@/lib/storage";

export default function Profiles() {
  const nav = useNavigate();
  const [profiles, setProfiles] = useState<any[]>([]);
  const [selected, setSelected] = useState<string[]>(() => loadWorkspace().selectedProfiles);
  const [status, setStatus] = useState<string>("");
  const [draftId, setDraftId] = useState<string>("");
  const [draftName, setDraftName] = useState<string>("");
  const [draftContent, setDraftContent] = useState<string>("");
  const [statusMap, setStatusMap] = useState<Record<string, string>>({});
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

  function getRegistryKey() {
    const vars = getVars();
    return vars["REGISTRY_API_KEY"] || vars["X_API_KEY"] || vars["API_KEY"] || "";
  }

  async function loadProfile(id: string) {
    setStatus("");
    try {
      const res = await fetch(`/api/profiles/${encodeURIComponent(id)}`);
      if (!res.ok) {
        setStatus(`Load failed: ${res.status}`);
        return;
      }
      const data = await res.json().catch(() => ({}));
      setDraftId(data?.id || id);
      setDraftName(data?.name || "");
      setDraftContent(data?.content || "");
      setStatus(`Loaded ${id}.`);
    } catch {
      setStatus("Load failed.");
    }
  }

  async function saveProfile() {
    setStatus("");
    const key = getRegistryKey();
    if (!key) {
      setStatus("Missing REGISTRY_API_KEY (set it in Settings).");
      return;
    }
    if (!draftId.trim()) {
      setStatus("Profile id is required.");
      return;
    }
    if (!draftContent.trim()) {
      setStatus("Profile content is required.");
      return;
    }
    const exists = profiles.some((p) => p.id === draftId.trim());
    const method = exists ? "PUT" : "POST";
    const url = exists ? `/api/profiles/${encodeURIComponent(draftId.trim())}` : "/api/profiles";
    const body = {
      id: draftId.trim(),
      name: draftName.trim() || draftId.trim(),
      version: "1.0",
      content: draftContent,
    };
    try {
      const res = await fetch(url, {
        method,
        headers: { "Content-Type": "application/json", "X-API-Key": key },
        body: JSON.stringify(body),
      });
      if (res.ok) {
        setStatus(exists ? "Updated." : "Created.");
        getAllProfiles().then(setProfiles);
        return;
      }
      const err = await res.json().catch(() => ({}));
      setStatus(`Save failed: ${err?.error || res.status}`);
    } catch {
      setStatus("Save failed.");
    }
  }

  async function deleteProfile(id: string) {
    setStatus("");
    const key = getRegistryKey();
    if (!key) {
      setStatus("Missing REGISTRY_API_KEY (set it in Settings).");
      return;
    }
    if (!confirm(`Delete profile ${id}?`)) return;
    try {
      const res = await fetch(`/api/profiles/${encodeURIComponent(id)}`, {
        method: "DELETE",
        headers: { "X-API-Key": key }
      });
      if (res.ok) {
        setStatus("Deleted.");
        getAllProfiles().then(setProfiles);
        return;
      }
      const err = await res.json().catch(() => ({}));
      setStatus(`Delete failed: ${err?.error || res.status}`);
    } catch {
      setStatus("Delete failed.");
    }
  }

  async function fetchStatus(id: string) {
    try {
      const res = await fetch(`/api/profiles/${encodeURIComponent(id)}/status`);
      if (!res.ok) {
        setStatusMap((prev) => ({ ...prev, [id]: `Status ${res.status}` }));
        return;
      }
      const data = await res.json().catch(() => ({}));
      const last = data?.last_run?.timestamp || data?.last_run?.created_at || data?.last_run?.ts || "";
      setStatusMap((prev) => ({ ...prev, [id]: last ? `Last run: ${last}` : "No runs yet" }));
    } catch {
      setStatusMap((prev) => ({ ...prev, [id]: "Status unavailable" }));
    }
  }

  async function callProfileAction(id: string, action: "pause" | "resume") {
    setStatus("");
    const key = getRegistryKey();
    if (!key) {
      setStatus("Missing REGISTRY_API_KEY (set it in Settings).");
      return;
    }
    try {
      const res = await fetch(`/api/profiles/${encodeURIComponent(id)}:${action}`, {
        method: "POST",
        headers: { "X-API-Key": key }
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        setStatus(`${action === "pause" ? "Paused" : "Resumed"} ${id}.`);
        fetchStatus(id);
        return;
      }
      setStatus(`Action failed: ${data?.error || res.status}`);
    } catch {
      setStatus("Action failed.");
    }
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
          <div key={p.id} style={{ padding: 12, borderRadius: 10, border: "1px solid #1f2a37", background: "#0f141c", display: "grid", gridTemplateColumns: "24px 24px 1fr 120px 220px", gap: 10, alignItems: "center" }}>
            <input type="checkbox" checked={selected.includes(p.id)} onChange={() => toggleSelect(p.id)} />
            <button onClick={() => togglePin(p.id)} style={{ border: "none", background: "transparent", color: pins.includes(p.id) ? "#ffd166" : "#666" }}>★</button>
            <div>
              <div style={{ fontWeight: 700 }}>{p.name || p.id}</div>
              <div style={{ fontSize: 12, opacity: 0.7 }}>{p.id}</div>
            </div>
            <div style={{ fontSize: 12, opacity: 0.7 }}>
              {p._virtual ? "imported" : "real"}
              <div style={{ fontSize: 11, opacity: 0.6, marginTop: 2 }}>{statusMap[p.id] || ""}</div>
            </div>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
              <button onClick={() => nav(`/charts?profiles=${encodeURIComponent(p.id)}`)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#111827", color: "#e5e7eb" }}>Open</button>
              {!p._virtual ? (
                <button onClick={() => loadProfile(p.id)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#1b2230", color: "#e5e7eb" }}>Edit</button>
              ) : null}
              {!p._virtual ? (
                <button onClick={() => fetchStatus(p.id)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#0f141c", color: "#e5e7eb" }}>Status</button>
              ) : null}
              {!p._virtual ? (
                <button onClick={() => callProfileAction(p.id, "pause")} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#1f1a12", color: "#ffd166" }}>Pause</button>
              ) : null}
              {!p._virtual ? (
                <button onClick={() => callProfileAction(p.id, "resume")} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#12201a", color: "#9ef0c0" }}>Resume</button>
              ) : null}
              {p._virtual ? (
                <button onClick={() => removeVirtual(p.id)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#222", color: "#fff" }}>Remove</button>
              ) : (
                <button onClick={() => deleteProfile(p.id)} style={{ padding: "6px 8px", borderRadius: 8, border: "1px solid #1f2a37", background: "#261313", color: "#ffb4b4" }}>Delete</button>
              )}
            </div>
          </div>
        ))}
      </div>

      <div style={{ marginTop: 8, padding: 12, borderRadius: 10, border: "1px solid #1f2a37", background: "#0f141c", display: "grid", gap: 8 }}>
        <div style={{ fontWeight: 700 }}>Create / Update Profile</div>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <input value={draftId} onChange={(e) => setDraftId(e.target.value)} placeholder="profile-id" style={{ padding: "8px 10px", borderRadius: 6, border: "1px solid #1f2a37", background: "#0b0c10", color: "#f3f4f6" }} />
          <input value={draftName} onChange={(e) => setDraftName(e.target.value)} placeholder="Profile name" style={{ padding: "8px 10px", borderRadius: 6, border: "1px solid #1f2a37", background: "#0b0c10", color: "#f3f4f6" }} />
        </div>
        <textarea value={draftContent} onChange={(e) => setDraftContent(e.target.value)} placeholder="YAML content" rows={8} style={{ padding: "8px 10px", borderRadius: 6, border: "1px solid #1f2a37", background: "#0b0c10", color: "#f3f4f6", fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, \"Liberation Mono\", \"Courier New\", monospace" }} />
        <div style={{ display: "flex", gap: 8 }}>
          <button onClick={saveProfile} style={{ padding: "8px 12px", borderRadius: 6, border: "1px solid #1f2a37", background: "#14161a", color: "#f3f4f6" }}>Save Profile</button>
          <button onClick={() => { setDraftId(""); setDraftName(""); setDraftContent(""); }} style={{ padding: "8px 12px", borderRadius: 6, border: "1px solid #1f2a37", background: "#1b1e26", color: "#f3f4f6" }}>Clear</button>
        </div>
        <div style={{ fontSize: 12, opacity: 0.6 }}>Requires `REGISTRY_API_KEY` in Settings for create/update/delete.</div>
      </div>

      {status ? <div style={{ fontSize: 12, opacity: 0.7 }}>{status}</div> : null}
    </div>
  );
}
