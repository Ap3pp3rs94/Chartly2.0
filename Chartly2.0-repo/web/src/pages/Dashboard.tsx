import React, { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useHeartbeat } from "@/App";

type ReportItem = {
  report_id: string;
  name: string;
  plan_hash: string;
  created_at: string;
};

type ReportList = { reports: ReportItem[] };

export default function Dashboard() {
  const hb = useHeartbeat();
  const nav = useNavigate();
  const [reports, setReports] = useState<ReportItem[]>([]);

  useEffect(() => {
    fetch("/api/reports")
      .then((r) => (r.ok ? r.json() : null))
      .then((d: ReportList | null) => setReports(d?.reports || []))
      .catch(() => setReports([]));
  }, []);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Dashboard</h1>
      <div style={{ fontSize: 12, opacity: 0.7 }}>Live status + quick actions.</div>

      <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #222", background: "#10151e", minWidth: 180 }}>
          <div style={{ fontSize: 12, opacity: 0.8 }}>Status</div>
          <div style={{ fontWeight: 700 }}>{hb?.status ?? "unknown"}</div>
          <div style={{ fontSize: 12, opacity: 0.6 }}>{hb?.ts ?? ""}</div>
        </div>
        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #222", background: "#10151e", minWidth: 180 }}>
          <div style={{ fontSize: 12, opacity: 0.8 }}>Profiles</div>
          <div style={{ fontWeight: 700 }}>{hb?.counts?.profiles ?? 0}</div>
        </div>
        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #222", background: "#10151e", minWidth: 180 }}>
          <div style={{ fontSize: 12, opacity: 0.8 }}>Drones</div>
          <div style={{ fontWeight: 700 }}>{hb?.counts?.drones ?? 0}</div>
        </div>
        <div style={{ padding: 12, borderRadius: 8, border: "1px solid #222", background: "#10151e", minWidth: 180 }}>
          <div style={{ fontSize: 12, opacity: 0.8 }}>Results</div>
          <div style={{ fontWeight: 700 }}>{hb?.counts?.results ?? 0}</div>
        </div>
      </div>

      <div style={{ display: "flex", gap: 10 }}>
        <button
          onClick={() => nav("/correlate")}
          style={{ padding: "8px 12px", borderRadius: 8, background: "#1f6feb", color: "#fff", border: "1px solid #1f6feb", cursor: "pointer" }}
        >
          New Report
        </button>
        <button
          onClick={() => nav("/charts")}
          style={{ padding: "8px 12px", borderRadius: 8, background: "#222", color: "#fff", border: "1px solid #333", cursor: "pointer" }}
        >
          Open Charts
        </button>
      </div>

      <div style={{ padding: 12, borderRadius: 8, border: "1px solid #222", background: "#10151e" }}>
        <div style={{ fontSize: 12, opacity: 0.8, marginBottom: 6 }}>Recent Reports</div>
        {reports.length === 0 ? (
          <div style={{ fontSize: 12, opacity: 0.6 }}>No saved reports yet.</div>
        ) : (
          <table style={{ width: "100%", fontSize: 12 }}>
            <thead>
              <tr style={{ textAlign: "left", opacity: 0.7 }}>
                <th>report_id</th>
                <th>name</th>
                <th>created_at</th>
              </tr>
            </thead>
            <tbody>
              {reports.map((r) => (
                <tr key={r.report_id}>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{r.report_id}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{r.name}</td>
                  <td style={{ borderTop: "1px solid #222", padding: "6px 0" }}>{r.created_at}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
