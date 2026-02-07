export function norm(s: string): string {
  return String(s ?? "").replaceAll("\u0000", "").trim();
}

export function normCollapse(s: string): string {
  const cleaned = norm(s);
  if (!cleaned) return "";
  return cleaned.split(/\s+/g).filter(Boolean).join(" ");
}

export function normalizeTenantId(s: string): string {
  const t = normCollapse(s);
  return t || "local";
}

export function isRFC3339(ts: string): boolean {
  const s = norm(ts);
  if (!s) return false;
  // Basic RFC3339 / RFC3339Nano check using Date.parse plus structural heuristics.
  // Accepts "YYYY-MM-DDTHH:MM:SSZ" and with fractional seconds and offset.
  const t = Date.parse(s);
  if (!Number.isFinite(t)) return false;
  // Must contain 'T' and timezone info (Z or +/-HH:MM)
  if (!s.includes("T")) return false;
  if (!(s.endsWith("Z") || /[+-]\d{2}:\d{2}$/.test(s))) return false;
  return true;
}

export function isUUIDLike(v: string): boolean {
  const s = norm(v);
  if (s.length !== 36) return false;
  // 8-4-4-4-12
  const parts = s.split("-");
  if (parts.length !== 5) return false;
  const lens = [8, 4, 4, 4, 12];
  for (let i = 0; i < parts.length; i++) {
    if (parts[i].length !== lens[i]) return false;
    if (!/^[0-9a-fA-F]+$/.test(parts[i])) return false;
  }
  return true;
}

export function isSafeKey(v: string): boolean {
  const s = norm(v);
  if (!s) return false;
  return /^[a-zA-Z0-9._-]+$/.test(s);
}

export function isPermission(v: string): boolean {
  const s = norm(v).toLowerCase();
  if (!s) return false;
  const parts = s.split(":");
  if (parts.length !== 3) return false;
  for (const seg of parts) {
    if (!seg) return false;
    // allow '*' only as entire segment
    if (seg === "*") continue;
    if (!/^[a-z0-9._-]+$/.test(seg)) return false;
  }
  return true;
}
