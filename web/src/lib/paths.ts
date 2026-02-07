export type FlatRecord = Record<string, any>;

export function normalizePath(path: string): string {
  return String(path || "").replace(/\[(\d+)\]/g, ".$1");
}

export function getByPath(obj: any, path: string): any {
  if (!path) return undefined;
  const parts = normalizePath(path).split(".").filter(Boolean);
  let cur = obj;
  for (const p of parts) {
    if (cur == null) return undefined;
    if (Array.isArray(cur) && /^\d+$/.test(p)) cur = cur[Number(p)];
    else if (typeof cur === "object") cur = cur[p];
    else return undefined;
  }
  return cur;
}

export function flatten(obj: any, prefix = "", out: FlatRecord = {}): FlatRecord {
  if (obj == null) return out;
  if (Array.isArray(obj)) {
    if (obj.length === 0) return out;
    const first = obj[0];
    if (typeof first === "object" && first !== null) {
      return flatten(first, prefix ? `${prefix}[0]` : "[0]", out);
    }
    out[prefix || "value"] = JSON.stringify(obj);
    return out;
  }
  if (typeof obj !== "object") {
    out[prefix] = obj;
    return out;
  }
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    const p = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object") flatten(v, p, out);
    else out[p] = v;
  }
  return out;
}

export function toNumber(v: any): number | null {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string") {
    const n = Number(v.replace(/,/g, "").trim());
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

export function parseTime(v: any): number | null {
  if (v == null) return null;
  if (typeof v === "number") return v > 1e12 ? v : v > 1e9 ? v * 1000 : v;
  if (typeof v === "string") {
    const n = Number(v);
    if (Number.isFinite(n)) return parseTime(n);
    const t = Date.parse(v);
    return Number.isFinite(t) ? t : null;
  }
  return null;
}
