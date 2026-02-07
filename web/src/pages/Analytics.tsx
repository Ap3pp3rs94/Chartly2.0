import React, { useEffect, useState } from "react";
import { getHealth } from "@/api/queries";
import LineChart from "@/components/charts/LineChart";
import BarChart from "@/components/charts/BarChart";
import Heatmap from "@/components/charts/Heatmap";

type State = "ok" | "error";

export default function Analytics() {
  const [state, setState] = useState<State>("error");
  const [detail, setDetail] = useState<any>(null);

  useEffect(() => {
    let alive = true;

    async function run() {
      const r = await getHealth("analytics");
      if (alive) {
        setState(r.state);
        setDetail(r.detail);
      }
    }

    run();
    const id = setInterval(run, 10000);
    return () => {
      alive = false;
      clearInterval(id);
    };
  }, []);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <div>
        <h1 style={{ margin: 0, fontSize: 20 }}>Analytics</h1>
        <div style={{ opacity: 0.75, fontSize: 12 }}>DATA ONLY  template catalog + chart previews</div>
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <strong>Service Health</strong>
        <div style={{ marginTop: 6 }}>
          status: {state === "ok" ? "OK" : "ERR"}{" "}
          <span style={{ opacity: 0.7, fontSize: 12 }}>{detail ? JSON.stringify(detail) : ""}</span>
        </div>
      </div>

      <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>
        <strong>Chart Templates Catalog</strong>
        <div style={{ opacity: 0.8, marginTop: 6 }}>
          Templates are defined as data-only presets in{" "}
          <code>services/analytics/internal/charts/templates/chart_templates.yaml</code>.
          A runtime template registry is a future implementation.
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 12 }}>
        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          <div style={{ fontWeight: 700, marginBottom: 8 }}>Preview  Line</div>
          <LineChart
            series={[
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
            ]}
            height={280}
            xLabel="Time"
            yLabel="Requests"
          />
        </div>

        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
          <div style={{ fontWeight: 700, marginBottom: 8 }}>Preview  Bar</div>
          <BarChart
            series={[
              { name: "north", points: [{ x: "Jan", y: 10 }, { x: "Feb", y: 14 }, { x: "Mar", y: 12 }] },
              { name: "south", points: [{ x: "Jan", y: 8 }, { x: "Feb", y: 12 }, { x: "Mar", y: 9 }] },
            ]}
            height={280}
            xLabel="Month"
            yLabel="Value"
          />
        </div>

        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 12, gridColumn: "1 / span 2" }}>
          <div style={{ fontWeight: 700, marginBottom: 8 }}>Preview  Heatmap</div>
          <Heatmap
            cells={[
              { x: "Mon", y: "A", value: 1 },
              { x: "Tue", y: "A", value: 2 },
              { x: "Wed", y: "A", value: 3 },
              { x: "Mon", y: "B", value: 2 },
              { x: "Tue", y: "B", value: 4 },
              { x: "Wed", y: "B", value: 6 },
              { x: "Mon", y: "C", value: 3 },
              { x: "Tue", y: "C", value: 6 },
              { x: "Wed", y: "C", value: 9 },
            ]}
            height={360}
            xLabel="Day"
            yLabel="Group"
          />
        </div>
      </div>
    </div>
  );
}
