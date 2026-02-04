import React, { useMemo } from "react";

export type Cell = { x: string; y: string; value: number };

export type HeatmapProps = {
  cells: Cell[];
  height?: number;
  xLabel?: string;
  yLabel?: string;
  maxX?: number;
  maxY?: number;
  showLegend?: boolean;
};

function safeString(v: unknown): string {
  return String(v ?? "").trim().replaceAll("\u0000", "");
}

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n));
}

function isFiniteNumber(n: unknown): n is number {
  return typeof n === "number" && Number.isFinite(n);
}

function grayFor(value: number, min: number, max: number): string {
  if (!Number.isFinite(value) || !Number.isFinite(min) || !Number.isFinite(max)) return "rgb(240,240,240)";
  if (max <= min) return "rgb(120,120,120)";
  const t = clamp((value - min) / (max - min), 0, 1);
  const g = Math.round(255 - t * 200);
  return `rgb(${g},${g},${g})`;
}

export default function Heatmap(props: HeatmapProps) {
  const height = props.height ?? 320;
  const maxX = props.maxX ?? 60;
  const maxY = props.maxY ?? 40;
  const showLegend = props.showLegend ?? true;

  const data = useMemo(() => {
    const raw = (props.cells ?? [])
      .filter((c) => c && typeof c.x === "string" && typeof c.y === "string" && isFiniteNumber(c.value))
      .map((c) => ({ x: safeString(c.x), y: safeString(c.y), value: c.value }));

    const xs = Array.from(new Set(raw.map((c) => c.x))).sort((a, b) => a.localeCompare(b));
    const ys = Array.from(new Set(raw.map((c) => c.y))).sort((a, b) => a.localeCompare(b));

    const xDomain = xs.slice(0, maxX > 0 ? maxX : xs.length);
    const yDomain = ys.slice(0, maxY > 0 ? maxY : ys.length);

    const allowedX = new Set(xDomain);
    const allowedY = new Set(yDomain);

    const included = raw.filter((c) => allowedX.has(c.x) && allowedY.has(c.y));

    let min = Infinity;
    let max = -Infinity;
    for (const c of included) {
      if (c.value < min) min = c.value;
      if (c.value > max) max = c.value;
    }
    if (!Number.isFinite(min)) min = 0;
    if (!Number.isFinite(max)) max = 0;

    const key = (x: string, y: string) => `${x}||${y}`;
    const map = new Map<string, number>();
    for (const c of included) map.set(key(c.x, c.y), c.value);

    return { xDomain, yDomain, map, min, max };
  }, [props.cells, maxX, maxY]);

  if (data.xDomain.length === 0 || data.yDomain.length === 0) {
    return <div style={{ padding: 12, border: "1px solid #ddd", borderRadius: 8 }}>No data</div>;
  }

  const padLeft = 60;
  const padTop = 30;
  const padRight = 20;
  const padBottom = 40;

  const width = 900;
  const innerW = width - padLeft - padRight;
  const innerH = height - padTop - padBottom;

  const cellW = innerW / data.xDomain.length;
  const cellH = innerH / data.yDomain.length;

  const titleId = "chartly-heatmap-title";
  const descId = "chartly-heatmap-desc";

  return (
    <div style={{ width: "100%", border: "1px solid #ddd", borderRadius: 8, padding: 12 }}>
      <svg
        role="img"
        aria-labelledby={`${titleId} ${descId}`}
        viewBox={`0 0 ${width} ${height}`}
        style={{ width: "100%", height }}
      >
        <title id={titleId}>Heatmap</title>
        <desc id={descId}>
          Heatmap of values across x and y categories. Darker cells represent higher values.
        </desc>

        {props.xLabel ? (
          <text x={padLeft + innerW / 2} y={height - 10} textAnchor="middle" fontSize="12">
            {props.xLabel}
          </text>
        ) : null}
        {props.yLabel ? (
          <text
            x={14}
            y={padTop + innerH / 2}
            textAnchor="middle"
            fontSize="12"
            transform={`rotate(-90 14 ${padTop + innerH / 2})`}
          >
            {props.yLabel}
          </text>
        ) : null}

        {data.yDomain.map((y, yi) => (
          <text
            key={y}
            x={padLeft - 8}
            y={padTop + yi * cellH + cellH / 2}
            textAnchor="end"
            dominantBaseline="middle"
            fontSize="10"
          >
            {y}
          </text>
        ))}

        {data.xDomain.map((x, xi) => {
          const show = data.xDomain.length <= 24 ? true : xi % Math.ceil(data.xDomain.length / 24) === 0;
          if (!show) return null;
          return (
            <text
              key={x}
              x={padLeft + xi * cellW + cellW / 2}
              y={padTop - 8}
              textAnchor="middle"
              fontSize="10"
            >
              {x}
            </text>
          );
        })}

        {data.yDomain.map((y, yi) =>
          data.xDomain.map((x, xi) => {
            const k = `${x}||${y}`;
            const v = data.map.get(k) ?? 0;
            const fill = grayFor(v, data.min, data.max);
            const cx = padLeft + xi * cellW;
            const cy = padTop + yi * cellH;
            const label = `x ${x}, y ${y}, value ${v}`;
            return (
              <rect
                key={k}
                x={cx}
                y={cy}
                width={cellW}
                height={cellH}
                fill={fill}
                stroke="rgba(0,0,0,0.08)"
                aria-label={label}
              >
                <title>{label}</title>
              </rect>
            );
          })
        )}

        {showLegend ? (
          <g transform={`translate(${padLeft}, ${height - padBottom + 10})`}>
            <text x={0} y={10} fontSize="10">
              min {data.min}
            </text>
            <rect x={60} y={0} width={120} height={12} fill="rgb(240,240,240)" stroke="rgba(0,0,0,0.15)" />
            {Array.from({ length: 24 }).map((_, i) => {
              const t = i / 23;
              const v = data.min + t * (data.max - data.min);
              return (
                <rect
                  key={i}
                  x={60 + i * (120 / 24)}
                  y={0}
                  width={120 / 24}
                  height={12}
                  fill={grayFor(v, data.min, data.max)}
                />
              );
            })}
            <text x={190} y={10} fontSize="10">
              max {data.max}
            </text>
          </g>
        ) : null}
      </svg>
    </div>
  );
}
