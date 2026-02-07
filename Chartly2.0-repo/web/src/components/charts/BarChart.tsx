import React, { useMemo } from "react";
import {
  Bar,
  BarChart as RCBarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

export type Point = { x: string; y: number };
export type Series = { name: string; points: Point[] };

export type BarChartProps = {
  series: Series[];
  height?: number;
  xLabel?: string;
  yLabel?: string;
  stacked?: boolean;
  stackId?: string;
  showLegend?: boolean;
  maxCategories?: number;
};

function isFiniteNumber(n: unknown): n is number {
  return typeof n === "number" && Number.isFinite(n);
}

function safeString(v: unknown): string {
  return String(v ?? "").trim().replaceAll("\u0000", "");
}

function normalizeSeries(series: Series[]): Series[] {
  const ss = (series ?? [])
    .filter((s) => s && typeof s.name === "string" && Array.isArray(s.points))
    .map((s) => ({
      name: s.name,
      points: (s.points ?? [])
        .filter((p) => p && typeof p.x === "string" && isFiniteNumber(p.y))
        .map((p) => ({ x: safeString(p.x), y: p.y })),
    }));
  ss.sort((a, b) => a.name.localeCompare(b.name));
  return ss;
}

function buildData(series: Series[], maxCategories: number): Array<Record<string, any>> {
  const catsSet = new Set<string>();
  for (const s of series) {
    for (const p of s.points) catsSet.add(p.x);
  }
  const cats = Array.from(catsSet);
  cats.sort((a, b) => a.localeCompare(b));

  const cap = maxCategories > 0 ? maxCategories : cats.length;
  const limited = cats.length > cap ? cats.slice(0, cap) : cats;

  const rowMap = new Map<string, Record<string, any>>();
  for (const c of limited) rowMap.set(c, { x: c });

  for (const s of series) {
    const pts = s.points.slice().sort((a, b) => a.x.localeCompare(b.x));
    for (const p of pts) {
      if (!rowMap.has(p.x)) continue;
      rowMap.get(p.x)![s.name] = p.y;
    }
  }

  return limited.map((c) => rowMap.get(c)!);
}

export default function BarChart(props: BarChartProps) {
  const height = props.height ?? 320;
  const stacked = props.stacked ?? false;
  const stackId = props.stackId ?? "default";
  const showLegend = props.showLegend ?? true;
  const maxCategories = props.maxCategories ?? 200;

  const safeSeries = useMemo(() => normalizeSeries(props.series ?? []), [props.series]);
  const data = useMemo(() => buildData(safeSeries, maxCategories), [safeSeries, maxCategories]);

  if (safeSeries.length === 0) {
    return <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>No data</div>;
  }

  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RCBarChart data={data}>
          <CartesianGrid strokeDasharray="3 3" />
          <XAxis dataKey="x" label={props.xLabel ? { value: props.xLabel, position: "insideBottom", offset: -5 } : undefined} />
          <YAxis label={props.yLabel ? { value: props.yLabel, angle: -90, position: "insideLeft" } : undefined} />
          <Tooltip />
          {showLegend ? <Legend /> : null}
          {safeSeries.map((s) => (
            <Bar key={s.name} dataKey={s.name} stackId={stacked ? stackId : undefined} />
          ))}
        </RCBarChart>
      </ResponsiveContainer>
    </div>
  );
}
