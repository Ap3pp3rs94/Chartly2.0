import React, { useEffect, useMemo, useRef } from "react";
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

export type StockPoint = { t: number; v: number };

type Props = {
  data: StockPoint[];
  color: string;
  height?: number;
};

function fmtTime(t: number) {
  const d = new Date(t);
  return `${d.getHours().toString().padStart(2, "0")}:${d.getMinutes().toString().padStart(2, "0")}`;
}

export default function StockChart({ data, color, height = 260 }: Props) {
  const chartData = useMemo(() => data.map((p) => ({ t: p.t, v: p.v })), [data]);
  const domainRef = useRef<{ min: number; max: number } | null>(null);

  useEffect(() => {
    if (!chartData.length) return;
    let min = chartData[0].v;
    let max = chartData[0].v;
    for (const p of chartData) {
      if (p.v < min) min = p.v;
      if (p.v > max) max = p.v;
    }
    if (!domainRef.current) {
      domainRef.current = { min, max };
      return;
    }
    const cur = domainRef.current;
    const lerp = (a: number, b: number, t: number) => a + (b - a) * t;
    domainRef.current = {
      min: lerp(cur.min, min, 0.2),
      max: lerp(cur.max, max, 0.2)
    };
  }, [chartData]);

  if (chartData.length === 0) {
    return <div style={{ height, display: "flex", alignItems: "center", justifyContent: "center", opacity: 0.6 }}>Waiting for data...</div>;
  }

  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={chartData} margin={{ top: 8, right: 12, left: 4, bottom: 4 }}>
          <XAxis dataKey="t" tickFormatter={fmtTime} stroke="#667085" />
          <YAxis
            stroke="#667085"
            width={40}
            domain={
              domainRef.current
                ? [domainRef.current.min, domainRef.current.max]
                : ["auto", "auto"]
            }
          />
          <Tooltip
            formatter={(v: any) => [Number(v).toFixed(2), "Value"]}
            labelFormatter={(l) => fmtTime(Number(l))}
            contentStyle={{ background: "#0f141c", border: "1px solid #1f2a37", borderRadius: 8 }}
            itemStyle={{ color: "#e5e7eb" }}
          />
          <Line type="monotone" dataKey="v" stroke={color} dot={false} strokeWidth={2} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
