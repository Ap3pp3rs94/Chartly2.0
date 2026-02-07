export type ReportType = "trend" | "compare" | "correlate" | "multi";

export type ReportSpec = {
  type: ReportType;
  profiles: string[];
  joinKey?: { path: string; label: string };
  measures: Array<{ profileId: string; path: string; label: string }>;
  rationale: string;
};

export type Workspace = {
  version: 1;
  selectedProfiles: string[];
  reportSpec?: ReportSpec;
};

const KEY = "chartly.workspace.v1";

type Subscriber = (ws: Workspace) => void;
const subs = new Set<Subscriber>();

const defaultWs: Workspace = { version: 1, selectedProfiles: [] };

export function loadWorkspace(): Workspace {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return { ...defaultWs };
    const parsed = JSON.parse(raw) as Workspace;
    if (!parsed || parsed.version !== 1) return { ...defaultWs };
    parsed.selectedProfiles = stableSort(parsed.selectedProfiles || []);
    return parsed;
  } catch {
    return { ...defaultWs };
  }
}

export function saveWorkspace(ws: Workspace): void {
  const copy: Workspace = {
    version: 1,
    selectedProfiles: stableSort(ws.selectedProfiles || []),
    reportSpec: ws.reportSpec
  };
  localStorage.setItem(KEY, JSON.stringify(copy));
  subs.forEach((fn) => fn(copy));
}

export function setSelectedProfiles(profileIds: string[]): Workspace {
  const ws = loadWorkspace();
  ws.selectedProfiles = stableSort(profileIds || []);
  saveWorkspace(ws);
  return ws;
}

export function setReportSpec(spec?: ReportSpec): Workspace {
  const ws = loadWorkspace();
  ws.reportSpec = spec;
  saveWorkspace(ws);
  return ws;
}

export function subscribe(fn: Subscriber): () => void {
  subs.add(fn);
  return () => subs.delete(fn);
}

function stableSort(arr: string[]): string[] {
  const uniq = Array.from(new Set(arr.map((s) => String(s))));
  uniq.sort((a, b) => a.localeCompare(b));
  return uniq;
}
