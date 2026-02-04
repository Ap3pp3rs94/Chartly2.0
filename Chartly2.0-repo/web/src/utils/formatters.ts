export function norm(s: string): string {
  return String(s ?? "").replaceAll("\u0000", "").trim();
}

export function normCollapse(s: string): string {
  const cleaned = norm(s);
  if (!cleaned) return "";
  return cleaned.split(/\s+/g).filter(Boolean).join(" ");
}

export function clamp(n: number, lo: number, hi: number): number {
  const x = Number(n);
  if (!Number.isFinite(x)) return lo;
  return Math.max(lo, Math.min(hi, x));
}

export function formatBytes(n: number): string {
  const x = Number(n);
  if (!Number.isFinite(x) || x < 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"] as const;
  let v = x;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v = v / 1024;
    i += 1;
  }
  // 1 decimal max, but only if needed.
  const fixed = v >= 10 || i === 0 ? Math.round(v) : Math.round(v * 10) / 10;
  return `${fixed} ${units[i]}`;
}

export function formatDurationMs(ms: number): string {
  const x = Number(ms);
  if (!Number.isFinite(x) || x < 0) return "0ms";
  if (x < 1000) return `${Math.round(x)}ms`;

  const s = x / 1000;
  if (s < 60) {
    const v = Math.round(s * 10) / 10; // 1 decimal max
    return `${v}s`;
  }

  const totalSec = Math.floor(s);
  const minutes = Math.floor(totalSec / 60);
  const seconds = totalSec % 60;

  if (minutes < 60) {
    return `${minutes}m ${seconds}s`;
  }

  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  return `${hours}h ${remMin}m ${seconds}s`;
}

export function formatISO(ts: string): string {
  const s = norm(ts);
  if (!s) return "";
  const t = Date.parse(s);
  if (!Number.isFinite(t)) return s;
  return new Date(t).toISOString();
}

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function stable(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(stable);
  if (isObject(v)) {
    const keys = Object.keys(v).sort((a, b) => a.localeCompare(b));
    const out: Record<string, unknown> = {};
    for (const k of keys) out[k] = stable((v as any)[k]);
    return out;
  }
  if (typeof v === "string") return norm(v);
  return v;
}

export function stableStringify(v: unknown, space: number = 2): string {
  try {
    return JSON.stringify(stable(v), null, space);
  } catch {
    return String(v);
  }
}
