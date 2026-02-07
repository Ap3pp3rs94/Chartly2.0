import React, { useMemo, useState } from "react";
import ReportBuilder, { ReportSpec } from "@/components/reports/ReportBuilder";
import ReportViewer from "@/components/reports/ReportViewer";
import ExportOptions from "@/components/reports/ExportOptions";

export default function Reports() {
  const [report, setReport] = useState<ReportSpec>(() => ({
    id: "report_1",
    title: "Example Report",
    description: "DATA ONLY  build/export/import report specs",
    charts: [
      { id: "chart_1", type: "line", title: "Requests", query: "requests", params: { window: "5d" } as Record<string, string>, templateId: "ts_line_v1" },
      { id: "chart_2", type: "bar", title: "Regions", query: "regions", params: {} as Record<string, string>, templateId: "bar_grouped_v1" },
      { id: "chart_3", type: "heatmap", title: "Heat", query: "heat", params: {} as Record<string, string>, templateId: "heatmap_v0" },
    ],
  }));

  // Deterministic sample data keyed by chart id.
  const data = useMemo(() => {
    return {
      chart_1: {
        series: [
          {
            name: "requests",
            points: [
              { x: "2026-01-01T00:00:00Z", y: 120 },
              { x: "2026-01-02T00:00:00Z", y: 160 },
              { x: "2026-01-03T00:00:00Z", y: 140 },
              { x: "2026-01-04T00:00:00Z", y: 200 },
              { x: "2026-01-05T00:00:00Z", y: 180 },
            ],
          },
        ],
      },
      chart_2: {
        series: [
          { name: "north", points: [{ x: "Jan", y: 10 }, { x: "Feb", y: 14 }, { x: "Mar", y: 12 }] },
          { name: "south", points: [{ x: "Jan", y: 8 }, { x: "Feb", y: 12 }, { x: "Mar", y: 9 }] },
        ],
      },
      chart_3: {
        cells: [
          { x: "Mon", y: "A", value: 1 },
          { x: "Tue", y: "A", value: 2 },
          { x: "Wed", y: "A", value: 3 },
          { x: "Mon", y: "B", value: 2 },
          { x: "Tue", y: "B", value: 4 },
          { x: "Wed", y: "B", value: 6 },
          { x: "Mon", y: "C", value: 3 },
          { x: "Tue", y: "C", value: 6 },
          { x: "Wed", y: "C", value: 9 },
        ],
      },
    } as Record<string, any>;
  }, []);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h1 style={{ margin: 0, fontSize: 20 }}>Reports</h1>
        <div style={{ opacity: 0.75, fontSize: 12 }}>Build and render report specs (client-side)</div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "1fr 420px", gap: 12 }}>
        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          {/* ReportBuilder is self-contained; we keep a separate state here for viewer/export */}
          <ReportBuilder />
          <div style={{ opacity: 0.7, fontSize: 12, marginTop: 8 }}>
            Note: This page also includes a viewer/export section using a deterministic example report state.
          </div>
        </div>

        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          <ExportOptions report={report} data={data} />
        </div>
      </div>

      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
          <div style={{ fontWeight: 700 }}>Viewer (example state)</div>
          <button
            onClick={() =>
              setReport((r) => ({
                ...r,
                title: r.title.endsWith(" *") ? r.title.slice(0, -2) : r.title + " *",
              }))
            }
            style={{ padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" }}
          >
            Toggle title marker
          </button>
        </div>

        <div style={{ marginTop: 10 }}>
          <ReportViewer report={report} data={data} />
        </div>
      </div>
    </div>
  );
}
