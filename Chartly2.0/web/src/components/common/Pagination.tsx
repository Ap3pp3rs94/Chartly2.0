import React, { useMemo } from "react";

export type PaginationProps = {
  page: number;
  totalPages: number;
  onChange: (page: number) => void;
  compact?: boolean;
};

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n));
}

export default function Pagination(props: PaginationProps) {
  const total = useMemo(() => Math.max(1, Math.floor(props.totalPages || 1)), [props.totalPages]);
  const page = useMemo(() => clamp(Math.floor(props.page || 1), 1, total), [props.page, total]);
  const compact = props.compact ?? false;

  function go(p: number) {
    props.onChange(clamp(Math.floor(p || 1), 1, total));
  }

  return (
    <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
      <button onClick={() => go(1)} disabled={page <= 1} style={btn()}>
        First
      </button>
      <button onClick={() => go(page - 1)} disabled={page <= 1} style={btn()}>
        Prev
      </button>

      <span style={{ fontSize: 12, opacity: 0.85 }}>
        page {page} / {total}
      </span>

      <button onClick={() => go(page + 1)} disabled={page >= total} style={btn()}>
        Next
      </button>
      <button onClick={() => go(total)} disabled={page >= total} style={btn()}>
        Last
      </button>

      {!compact ? (
        <span style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, opacity: 0.85 }}>
          Go to
          <input
            type="number"
            value={page}
            min={1}
            max={total}
            onChange={(e) => go(Number(e.target.value))}
            style={{ padding: "6px 8px", borderRadius: 6, border: "1px solid #ccc", width: 80 }}
          />
        </span>
      ) : null}
    </div>
  );
}

function btn(): React.CSSProperties {
  return { padding: "6px 10px", borderRadius: 8, border: "1px solid #ccc", background: "#fff", cursor: "pointer" };
}
