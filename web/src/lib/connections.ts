export type ConnectionTemplate = {
  id: string;
  label: string;
  vars: Array<{ name: string; optional?: boolean }>;
};

export const connectionTemplates: ConnectionTemplate[] = [];

export function normalizeVarName(name: string): string {
  return String(name || "").trim().toUpperCase().replace(/[^A-Z0-9_]/g, "_");
}

export function varsToHeaders(vars: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = {};
  for (const [k, v] of Object.entries(vars || {})) {
    const key = normalizeVarName(k);
    if (!v) continue;
    headers[`X-Chartly-Var-${key}`] = v;
  }
  return headers;
}
