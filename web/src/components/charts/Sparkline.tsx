import React from "react";
import { Line, LineChart, ResponsiveContainer } from "recharts";

type Props = {
  data: Array<{ t: number; v: number }>;
  color: string;
  height?: number;
};

export default function Sparkline({ data, color, height = 40 }: Props) {
  if (!data || data.length === 0) return <div style={{ height }} />;
  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={data} margin={{ top: 2, right: 2, left: 2, bottom: 2 }}>
          <Line type="monotone" dataKey="v" stroke={color} dot={false} strokeWidth={2} isAnimationActive={false} />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
