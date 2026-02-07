import React, { useEffect, useState } from "react";
import { recommendReport, ReportSpec } from "@/lib/reportAutopilot";
import { setReportSpec } from "@/lib/workspace";
import { useNavigate } from "react-router-dom";

type Props = { profiles: string[] };

export default function ReportAutopilotCard({ profiles }: Props) {
  const [spec, setSpec] = useState<ReportSpec | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const nav = useNavigate();

  useEffect(() => {
    if (!profiles.length) {
      setSpec(undefined);
      setReportSpec(undefined);
      return;
    }
    setLoading(true);
    setError("");
    recommendReport(profiles)
      .then((s) => {
        setSpec(s);
        if (s) setReportSpec(s);
      })
      .catch(() => {
        setError("Couldn't inspect fields right now. You can still view charts.");
      })
      .finally(() => setLoading(false));
  }, [profiles.join(",")]);

  return (
    <div className="card">
      <div className="h1">Autopilot Report</div>
      {loading ? <div className="hint">Analyzing datasets</div> : null}
      {error ? <div className="hint">{error}</div> : null}
      {spec ? (
        <div className="stack">
          <div className="hint">Recommended: <b>{spec.type}</b></div>
          {spec.joinKey ? <div className="hint">Join key: {spec.joinKey.label}</div> : null}
          <div className="hint">Measures:</div>
          <ul className="listPlain">
            {spec.measures.map((m) => (
              <li key={`${m.profileId}-${m.path}`}>{m.profileId}: {m.label}</li>
            ))}
          </ul>
          <div className="hint">{spec.rationale}</div>
        </div>
      ) : null}
      <div className="row" style={{ marginTop: 12 }}>
        <button className="btn" onClick={() => nav("/charts")}>Run in Charts</button>
        <button className="btn ghost" onClick={() => setReportSpec(undefined)}>Clear report</button>
      </div>
    </div>
  );
}
