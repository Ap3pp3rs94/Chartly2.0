export type FieldInfo = { path: string; label: string; type: "string"|"number"|"boolean"|"array"; sample?: any };

export type ReportType = "trend" | "compare" | "correlate" | "multi";
export type ReportSpec = {
  type: ReportType;
  profiles: string[];
  joinKey?: { path: string; label: string };
  measures: Array<{ profileId: string; path: string; label: string }>;
  rationale: string;
};

type CacheEntry = { expires: number; fields: FieldInfo[] };
const cache = new Map<string, CacheEntry>();
const TTL = 5 * 60 * 1000;

export async function fetchProfileFields(profileId: string): Promise<FieldInfo[]> {
  const now = Date.now();
  const hit = cache.get(profileId);
  if (hit && hit.expires > now) return hit.fields;
  const res = await fetch(`/api/profiles/${encodeURIComponent(profileId)}/fields`);
  if (!res.ok) throw new Error(`fields_failed_${res.status}`);
  const data = await res.json();
  const fields = Array.isArray(data?.fields) ? data.fields : [];
  const clean = fields.map((f: any) => ({
    path: String(f.path || ""),
    label: String(f.label || f.path || ""),
    type: (f.type || "string") as FieldInfo["type"],
    sample: f.sample
  })).filter((f: FieldInfo) => f.path);
  cache.set(profileId, { expires: now + TTL, fields: clean });
  return clean;
}

export async function recommendReport(profiles: string[]): Promise<ReportSpec | undefined> {
  const list = stableSort(profiles);
  if (!list.length) return undefined;

  const fieldsByProfile: Record<string, FieldInfo[]> = {};
  for (const id of list) {
    try {
      fieldsByProfile[id] = await fetchProfileFields(id);
    } catch {
      fieldsByProfile[id] = [];
    }
  }

  if (list.length === 1) {
    const id = list[0];
    const measures = pickMeasures(id, fieldsByProfile[id]);
    return {
      type: "trend",
      profiles: list,
      measures,
      rationale: measures.length ? "Single dataset trend with best numeric measure." : "Limited schema; showing multi view."
    };
  }

  const joinKey = pickJoinKey(list, fieldsByProfile);
  const measures = list.flatMap((id) => pickMeasures(id, fieldsByProfile[id]).slice(0, 1));

  if (joinKey) {
    return {
      type: "correlate",
      profiles: list,
      joinKey,
      measures,
      rationale: `Correlating datasets on ${joinKey.label}.`
    };
  }

  return {
    type: "multi",
    profiles: list,
    measures,
    rationale: "Multi dataset view (no reliable join key found)."
  };
}

function pickMeasures(profileId: string, fields: FieldInfo[]) {
  const nums = fields.filter((f) => f.type === "number");
  const scored = nums.map((f) => ({ f, score: scoreMetric(f) }))
    .sort((a, b) => b.score - a.score || a.f.label.localeCompare(b.f.label) || a.f.path.localeCompare(b.f.path));
  return scored.slice(0, 2).map((s) => ({ profileId, path: s.f.path, label: s.f.label }));
}

function scoreMetric(f: FieldInfo) {
  const p = (f.label + " " + f.path).toLowerCase();
  const keys = ["rate","total","count","value","amount","price","volume","population","percent","pct","index"];
  let score = 1;
  for (const k of keys) if (p.includes(k)) score += 2;
  return score;
}

function pickJoinKey(profiles: string[], fieldsByProfile: Record<string, FieldInfo[]>) {
  const sets = profiles.map((id) => stringFields(fieldsByProfile[id] || []));
  if (!sets.length) return undefined;
  // intersection by normalized label/path
  const first = sets[0];
  const common = new Map<string, { path: string; label: string; score: number }>();
  for (const f of first) {
    const key = normalizeKey(f);
    common.set(key, { path: f.path, label: f.label, score: scoreJoin(f) });
  }
  for (let i = 1; i < sets.length; i++) {
    const cur = new Map<string, { path: string; label: string; score: number }>();
    for (const f of sets[i]) {
      const key = normalizeKey(f);
      if (common.has(key)) {
        const prev = common.get(key)!;
        const s = Math.max(prev.score, scoreJoin(f));
        cur.set(key, { path: prev.path, label: prev.label, score: s });
      }
    }
    for (const k of common.keys()) if (!cur.has(k)) common.delete(k);
    for (const [k, v] of cur.entries()) common.set(k, v);
  }
  const items = Array.from(common.values());
  if (!items.length) return undefined;
  items.sort((a, b) => b.score - a.score || a.label.localeCompare(b.label) || a.path.localeCompare(b.path));
  const best = items[0];
  return { path: best.path, label: best.label };
}

function stringFields(fields: FieldInfo[]) {
  return fields.filter((f) => f.type === "string");
}

function normalizeKey(f: FieldInfo) {
  const last = (f.label || f.path).toLowerCase().trim();
  return last;
}

function scoreJoin(f: FieldInfo) {
  const p = (f.label + " " + f.path).toLowerCase();
  const keys = ["id","code","symbol","name","state","country","zip"];
  let score = 1;
  for (const k of keys) if (p.includes(k)) score += 2;
  return score;
}

function stableSort(arr: string[]): string[] {
  const uniq = Array.from(new Set(arr.map((s) => String(s))));
  uniq.sort((a, b) => a.localeCompare(b));
  return uniq;
}
