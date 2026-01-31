import React, { useMemo } from "react";
import LineChart, { Series as LineSeries } from "@/components/charts/LineChart";
import BarChart, { Series as BarSeries } from "@/components/charts/BarChart";
import Heatmap, { Cell as HeatCell } from "@/components/charts/Heatmap";
import type { ReportSpec, ChartSpec } from "./ReportBuilder";

type ReportViewerProps = {
  report: ReportSpec;
  data?: Record<string, any>;
};

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
}

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function sortCharts(charts: ChartSpec[]): ChartSpec[] {
  const cp = (charts ?? []).slice();
  cp.sort((a, b) => safeString(a.id).localeCompare(safeString(b.id)));
  return cp;
}

function asLineData(v: any): { series: LineSeries[] } | null {
  if (!isObject(v)) return null;
  const series = v.series;
  if (!Array.isArray(series)) return null;
  return { series: series as LineSeries[] };
}

function asBarData(v: any): { series: BarSeries[] } | null {
  if (!isObject(v)) return null;
  const series = v.series;
  if (!Array.isArray(series)) return null;
  return { series: series as BarSeries[] };
}

function asHeatData(v: any): { cells: HeatCell[] } | null {
  if (!isObject(v)) return null;
  const cells = v.cells;
  if (!Array.isArray(cells)) return null;
  return { cells: cells as HeatCell[] };
}

export default function ReportViewer(props: ReportViewerProps) {
  const charts = useMemo(() => sortCharts(props.report?.charts ?? []), [props.report]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
        <div style={{ fontWeight: 700, fontSize: 18 }}>{props.report?.title ?? "Report"}</div>
        {props.report?.description ? (
          <div style={{ opacity: 0.75, marginTop: 6 }}>{props.report.description}</div>
        ) : null}
      </div>

      {charts.length === 0 ? (
        <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8, opacity: 0.75 }}>
          No charts in report.
        </div>
      ) : null}

      {charts.map((c) => {
        const dataset = props.data?.[c.id];
        return (
          <div key={c.id} style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
            <div style={{ display: "flex", justifyContent: "space-between", gap: 10, flexWrap: "wrap" }}>
              <div>
                <div style={{ fontWeight: 700 }}>{c.title}</div>
                <div style={{ opacity: 0.7, fontSize: 12 }}>
                  id: {c.id}  type: {c.type}  template: {c.templateId ?? "(none)"}  query: {c.query || "(none)"}
                </div>
              </div>
            </div>

            <div style={{ marginTop: 10 }}>
              {c.type === "line" ? (
                (() => {
                  const d = asLineData(dataset);
                  return d ? (
                    <LineChart series={d.series} height={320} xLabel="x" yLabel="y" />
                  ) : (
                    <Placeholder id={c.id} />
                  );
                })()
              ) : c.type === "bar" ? (
                (() => {
                  const d = asBarData(dataset);
                  return d ? (
                    <BarChart series={d.series} height={320} xLabel="x" yLabel="y" />
                  ) : (
                    <Placeholder id={c.id} />
                  );
                })()
              ) : c.type === "heatmap" ? (
                (() => {
                  const d = asHeatData(dataset);
                  return d ? <Heatmap cells={d.cells} height={340} xLabel="x" yLabel="y" /> : <Placeholder id={c.id} />;
                })()
              ) : (
                <div style={{ opacity: 0.75 }}>Unsupported chart type.</div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}

function Placeholder({ id }: { id: string }) {
  return (
    <div style={{ padding: 12, border: "1px dashed #bbb", borderRadius: 8, opacity: 0.8 }}>
      No dataset provided for chart id: <strong>{id}</strong>
      <div style={{ marginTop: 6, fontSize: 12 }}>
        Provide data as props: <code>data&#123;{id}: ...&#125;</code>
      </div>
    </div>
  );
}
