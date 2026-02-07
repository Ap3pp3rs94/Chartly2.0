import React, { useState } from "react";
import VarsEditor from "@/components/Settings/VarsEditor";
import { getSettings, setSettings } from "@/lib/storage";

export default function Settings() {
  const current = getSettings();
  const [refreshMs, setRefreshMs] = useState<number>(current.refreshMs);
  const [summaryCadenceMin, setSummaryCadenceMin] = useState<number>(current.summaryCadenceMin);
  const [defaultRange, setDefaultRange] = useState<string>(current.defaultRange);
  const [saved, setSaved] = useState<string>("");
  const [cleared, setCleared] = useState<string>("");

  function savePrefs() {
    setSettings({ refreshMs, summaryCadenceMin, defaultRange });
    setSaved("Saved.");
    setTimeout(() => setSaved(""), 1500);
  }

  function clearLocalData() {
    const keys = [
      "chartly.settings.v1",
      "chartly.vars.v1",
      "chartly.profiles.virtual.v1",
      "chartly.workspace.v1",
      "chartly_last_insights_hash",
      "chartly_selected_profiles",
      "chartly_watchlist",
      "chartly_autopilot",
      "chartly_ui_advanced",
      "chartly_last_insights_hash"
    ];
    for (const k of keys) {
      try {
        localStorage.removeItem(k);
        sessionStorage.removeItem(k);
      } catch {
        // ignore
      }
    }
    setCleared("Local cache cleared. Reloading...");
    setTimeout(() => {
      window.location.reload();
    }, 700);
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Settings</h1>

      <VarsEditor />

      <div style={{ padding: 14, borderRadius: 6, border: "1px solid #1f2228", background: "#0f1115" }}>
        <div style={{ fontWeight: 800, marginBottom: 8 }}>Preferences</div>
        <div style={{ display: "grid", gridTemplateColumns: "200px 1fr", gap: 8, alignItems: "center" }}>
          <label style={{ fontSize: 12, opacity: 0.8 }}>Refresh interval (ms)</label>
          <input
            type="number"
            value={refreshMs}
            onChange={(e) => setRefreshMs(Number(e.target.value) || 25000)}
            style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6", width: 160 }}
          />

          <label style={{ fontSize: 12, opacity: 0.8 }}>Summary cadence (minutes)</label>
          <select
            value={summaryCadenceMin}
            onChange={(e) => setSummaryCadenceMin(Number(e.target.value))}
            style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6", width: 160 }}
          >
            <option value={5}>5</option>
            <option value={10}>10</option>
            <option value={30}>30</option>
          </select>

          <label style={{ fontSize: 12, opacity: 0.8 }}>Default time range</label>
          <select
            value={defaultRange}
            onChange={(e) => setDefaultRange(e.target.value)}
            style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6", width: 160 }}
          >
            <option value="last_hour">Last hour</option>
            <option value="today">Today</option>
            <option value="last_7d">Last 7 days</option>
          </select>
        </div>
        <div style={{ marginTop: 10, display: "flex", alignItems: "center", gap: 10 }}>
          <button
            onClick={savePrefs}
            style={{ padding: "8px 12px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}
          >
            Save Preferences
          </button>
          {saved ? <div style={{ fontSize: 12, opacity: 0.7 }}>{saved}</div> : null}
        </div>
      </div>

      <div style={{ padding: 14, borderRadius: 6, border: "1px solid #2a0f10", background: "#120a0b" }}>
        <div style={{ fontWeight: 800, marginBottom: 8 }}>Reset Local Data</div>
        <div style={{ fontSize: 12, opacity: 0.7, marginBottom: 10 }}>
          Clears cached selections, virtual profiles, and UI state in this browser only.
        </div>
        <button
          onClick={clearLocalData}
          style={{ padding: "8px 12px", borderRadius: 4, border: "1px solid #4a1b1c", background: "#1a0b0c", color: "#ffb4b4" }}
        >
          Clear Local Cache
        </button>
        {cleared ? <div style={{ fontSize: 12, opacity: 0.7, marginTop: 8 }}>{cleared}</div> : null}
      </div>
    </div>
  );
}
