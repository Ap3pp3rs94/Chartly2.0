import React, { useMemo } from "react";
import {
  CartesianGrid,
  Line,
  LineChart as RCLineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
  Legend,
} from "recharts";

export type Point = { x: string | number; y: number };
export type Series = { name: string; points: Point[] };

export type LineChartProps = {
  series: Series[];
  height?: number;
  xLabel?: string;
  yLabel?: string;
  showLegend?: boolean;
  maxPoints?: number;
};

function isFiniteNumber(n: unknown): n is number {
  return typeof n === "number" && Number.isFinite(n);
}

function safeString(v: unknown): string {
  return String(v ?? "");
}

function parseTimeMaybe(s: string): number | null {
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

function sortPoints(points: Point[]): Point[] {
  const cp = points.slice();

  const first = cp[0];
  if (!first) return cp;

  if (typeof first.x === "number") {
    cp.sort((a, b) => {
      const ax = typeof a.x === "number" ? a.x : Number.NaN;
      const bx = typeof b.x === "number" ? b.x : Number.NaN;
      if (!Number.isFinite(ax) && !Number.isFinite(bx)) return 0;
      if (!Number.isFinite(ax)) return 1;
      if (!Number.isFinite(bx)) return -1;
      return ax - bx;
    });
    return cp;
  }

  const t0 = parseTimeMaybe(String(first.x));
  if (t0 !== null) {
    cp.sort((a, b) => {
      const at = parseTimeMaybe(String(a.x));
      const bt = parseTimeMaybe(String(b.x));
      if (at === null && bt === null) return safeString(a.x).localeCompare(safeString(b.x));
      if (at === null) return 1;
      if (bt === null) return -1;
      return at - bt;
    });
    return cp;
  }

  cp.sort((a, b) => safeString(a.x).localeCompare(safeString(b.x)));
  return cp;
}

function downsample(points: Point[], maxPoints: number): Point[] {
  if (maxPoints <= 0) return points.slice();
  const n = points.length;
  if (n <= maxPoints) return points.slice();
  const stride = Math.ceil(n / maxPoints);
  const out: Point[] = [];
  for (let i = 0; i < n; i += stride) out.push(points[i]);
  if (out.length > 0 && out[out.length - 1] !== points[n - 1]) out.push(points[n - 1]);
  return out;
}

function flattenSeries(series: Series[], maxPoints: number): Array<Record<string, any>> {
  const rowMap = new Map<string, Record<string, any>>();
  const orderedXs: Point[] = [];

  for (const s of series) {
    const sorted = sortPoints(s.points ?? []);
    const ds = downsample(sorted, maxPoints);
    for (const p of ds) {
      const key = safeString(p.x);
      if (!rowMap.has(key)) {
        rowMap.set(key, { x: p.x });
        orderedXs.push({ x: p.x, y: 0 });
      }
      rowMap.get(key)![s.name] = p.y;
    }
  }

  const sortedXs = sortPoints(orderedXs);
  const out: Array<Record<string, any>> = [];
  for (const p of sortedXs) {
    const key = safeString(p.x);
    const row = rowMap.get(key);
    if (row) out.push(row);
  }
  return out;
}

export default function LineChart(props: LineChartProps) {
  const height = props.height ?? 320;
  const showLegend = props.showLegend ?? true;
  const maxPoints = props.maxPoints ?? 5000;

  const safeSeries = useMemo(() => {
    const ss = (props.series ?? [])
      .filter((s) => s && typeof s.name === "string" && Array.isArray(s.points))
      .map((s) => ({
        name: s.name,
        points: (s.points ?? []).filter((p) => p && (typeof p.x === "string" || typeof p.x === "number") && isFiniteNumber(p.y)),
      }));
    ss.sort((a, b) => a.name.localeCompare(b.name));
    return ss;
  }, [props.series]);

  const data = useMemo(() => flattenSeries(safeSeries, maxPoints), [safeSeries, maxPoints]);

  if (safeSeries.length === 0) {
    return <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>No data</div>;
  }

  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RCLineChart data={data}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="x" label={props.xLabel ? { value: props.xLabel, position: "insideBottom", offset: -5 } : undefined} />
          <YAxis label={props.yLabel ? { value: props.yLabel, angle: -90, position: "insideLeft" } : undefined} />
          <Tooltip />
          {showLegend ? <Legend /> : null}
          {safeSeries.map((s) => (
            <Line key={s.name} type="monotone" dataKey={s.name} dot={false} strokeWidth={2} />
          ))}
        </RCLineChart>
      </ResponsiveContainer>
    </div>
  );
}
