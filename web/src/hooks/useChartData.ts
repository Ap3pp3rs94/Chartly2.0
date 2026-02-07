import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/api/client";

export type ChartType = "line" | "bar" | "heatmap";

export type ChartRequest = {
  type: ChartType;
  service?: "analytics" | "observer" | "audit" | "storage" | "gateway";
  path: string;
  method?: "GET" | "POST";
  params?: Record<string, string | number>;
  body?: any;
};

export type LineDataset = {
  series: { name: string; points: { x: string | number; y: number }[] }[];
};

export type BarDataset = {
  series: { name: string; points: { x: string; y: number }[] }[];
};

export type HeatDataset = {
  cells: { x: string; y: string; value: number }[];
};

export type ChartDataset = LineDataset | BarDataset | HeatDataset;

export type UseChartDataOptions = {
  enabled?: boolean; // default true
  pollMs?: number; // default 0
  timeoutMs?: number; // default 5000
  fetcher?: (req: ChartRequest) => Promise<any>;
};

function isObject(v: unknown): v is Record<string, any> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function safeString(v: unknown): string {
  return String(v ?? "").replaceAll("\u0000", "").trim();
}

function safeNumber(v: unknown): number | null {
  const n = typeof v === "number" ? v : Number(v);
  return Number.isFinite(n) ? n : null;
}

function parseTimeMaybe(s: string): number | null {
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

function buildQuery(params?: Record<string, string | number>): string {
  if (!params) return "";
  const keys = Object.keys(params).filter((k) => params[k] !== undefined);
  keys.sort((a, b) => a.localeCompare(b));
  const usp = new URLSearchParams();
  for (const k of keys) usp.set(k, String(params[k]));
  const s = usp.toString();
  return s ? `?${s}` : "";
}

function unwrapPayload(raw: any): any {
  if (!raw) return raw;
  if (isObject(raw)) {
    if ("data" in raw) return (raw as any).data;
    if ("items" in raw) return (raw as any).items;
    if ("result" in raw) return (raw as any).result;
  }
  return raw;
}

function normalizeLine(raw: any): LineDataset | null {
  const v = unwrapPayload(raw);
  if (!isObject(v) || !Array.isArray((v as any).series)) return null;

  const series = ((v as any).series as any[])
    .filter((s) => isObject(s) && typeof (s as any).name === "string" && Array.isArray((s as any).points))
    .map((s) => {
      const name = safeString((s as any).name);
      const points = ((s as any).points as any[])
        .filter((p) => isObject(p) && ("x" in (p as any)) && ("y" in (p as any)))
        .map((p) => {
          const y = safeNumber((p as any).y);
          if (y === null) return null;
          const xRaw = (p as any).x;
          const x = typeof xRaw === "number" ? xRaw : safeString(xRaw);
          return { x, y };
        })
        .filter(Boolean) as { x: string | number; y: number }[];

      // Deterministic sort by x
      const sorted = points.slice();
      const first = sorted[0];
      if (first) {
        if (typeof first.x === "number") {
          sorted.sort((a, b) => {
            const ax = typeof a.x === "number" ? a.x : Number.NaN;
            const bx = typeof b.x === "number" ? b.x : Number.NaN;
            if (!Number.isFinite(ax) && !Number.isFinite(bx)) return 0;
            if (!Number.isFinite(ax)) return 1;
            if (!Number.isFinite(bx)) return -1;
            return ax - bx;
          });
        } else {
          const t0 = parseTimeMaybe(String(first.x));
          if (t0 !== null) {
            sorted.sort((a, b) => {
              const at = parseTimeMaybe(String(a.x));
              const bt = parseTimeMaybe(String(b.x));
              if (at === null && bt === null) return safeString(a.x).localeCompare(safeString(b.x));
              if (at === null) return 1;
              if (bt === null) return -1;
              return at - bt;
            });
          } else {
            sorted.sort((a, b) => safeString(a.x).localeCompare(safeString(b.x)));
          }
        }
      }

      return { name, points: sorted };
    })
    .filter((s) => s.name.length > 0);

  series.sort((a, b) => a.name.localeCompare(b.name));
  return { series };
}

function normalizeBar(raw: any): BarDataset | null {
  const v = unwrapPayload(raw);
  if (!isObject(v) || !Array.isArray((v as any).series)) return null;

  const series = ((v as any).series as any[])
    .filter((s) => isObject(s) && typeof (s as any).name === "string" && Array.isArray((s as any).points))
    .map((s) => {
      const name = safeString((s as any).name);
      const points = ((s as any).points as any[])
        .filter((p) => isObject(p) && typeof (p as any).x === "string" && ("y" in (p as any)))
        .map((p) => {
          const x = safeString((p as any).x);
          const y = safeNumber((p as any).y);
          if (!x || y === null) return null;
          return { x, y };
        })
        .filter(Boolean) as { x: string; y: number }[];

      // Deterministic category order
      points.sort((a, b) => a.x.localeCompare(b.x));
      return { name, points };
    })
    .filter((s) => s.name.length > 0);

  series.sort((a, b) => a.name.localeCompare(b.name));
  return { series };
}

function normalizeHeat(raw: any): HeatDataset | null {
  const v = unwrapPayload(raw);
  if (!isObject(v) || !Array.isArray((v as any).cells)) return null;

  const cells = ((v as any).cells as any[])
    .filter((c) => isObject(c) && typeof (c as any).x === "string" && typeof (c as any).y === "string")
    .map((c) => {
      const x = safeString((c as any).x);
      const y = safeString((c as any).y);
      const value = safeNumber((c as any).value);
      if (!x || !y || value === null) return null;
      return { x, y, value };
    })
    .filter(Boolean) as { x: string; y: string; value: number }[];

  // Deterministic ordering by y asc then x asc then value
  cells.sort((a, b) => {
    if (a.y !== b.y) return a.y.localeCompare(b.y);
    if (a.x !== b.x) return a.x.localeCompare(b.x);
    return a.value - b.value;
  });

  return { cells };
}

function normalizeDataset(req: ChartRequest, raw: any): ChartDataset | null {
  if (req.type === "line") return normalizeLine(raw);
  if (req.type === "bar") return normalizeBar(raw);
  if (req.type === "heatmap") return normalizeHeat(raw);
  return null;
}

async function defaultFetcher(req: ChartRequest, timeoutMs: number): Promise<any> {
  const svc = (req.service ?? "analytics") as any;
  const method = (req.method ?? "GET").toUpperCase();
  const p = req.path.startsWith("/") ? req.path : `/${req.path}`;
  const q = buildQuery(req.params);
  const path = `${p}${q}`;

  if (method === "POST") {
    return api.post<any>(svc, path, req.body ?? {});
  }
  return api.get<any>(svc, path);
}

export function useChartData(
  req: ChartRequest | null,
  opts?: UseChartDataOptions
): { data: ChartDataset | null; error: string | null; loading: boolean; refresh: () => void } {
  const enabled = opts?.enabled !== false;
  const pollMs = opts?.pollMs ?? 0;
  const timeoutMs = opts?.timeoutMs ?? 5000;

  const fetcherRef = useRef<(r: ChartRequest) => Promise<any>>(
    opts?.fetcher ? opts.fetcher : (r) => defaultFetcher(r, timeoutMs)
  );

  useEffect(() => {
    fetcherRef.current = opts?.fetcher ? opts.fetcher : (r) => defaultFetcher(r, timeoutMs);
  }, [opts?.fetcher, timeoutMs]);

  const key = useMemo(() => {
    if (!req) return "null";
    // Deterministic request key (sorted params)
    const paramsKeys = req.params ? Object.keys(req.params).sort((a, b) => a.localeCompare(b)) : [];
    const paramsStr = paramsKeys.map((k) => `${k}=${String(req.params?.[k])}`).join("&");
    const method = (req.method ?? "GET").toUpperCase();
    const svc = req.service ?? "analytics";
    // Body is not stringified here to avoid heavy deps; for changes caller should pass stable object identity or change key externally.
    return `${req.type}|${svc}|${method}|${req.path}|${paramsStr}`;
  }, [req]);

  const [data, setData] = useState<ChartDataset | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(false);

  const aliveRef = useRef(true);
  const timerRef = useRef<number | null>(null);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const run = useCallback(async () => {
    if (!enabled || !req) return;
    setLoading(true);
    try {
      const raw = await fetcherRef.current(req);
      if (!aliveRef.current) return;

      const norm = normalizeDataset(req, raw);
      if (!norm) {
        setData(null);
        setError("Invalid dataset shape (data-only endpoint may not be implemented yet).");
      } else {
        setData(norm);
        setError(null);
      }
    } catch (e: any) {
      if (!aliveRef.current) return;
      setData(null);
      setError(String(e?.message ?? e));
    } finally {
      if (aliveRef.current) setLoading(false);
    }
  }, [enabled, req]);

  useEffect(() => {
    aliveRef.current = true;
    clearTimer();

    if (!enabled || !req) {
      setLoading(false);
      return () => {
        aliveRef.current = false;
        clearTimer();
      };
    }

    // Immediate fetch
    run();

    // Optional polling
    if (pollMs > 0) {
      const ms = Math.max(250, Math.floor(pollMs));
      timerRef.current = window.setInterval(run, ms);
    }

    return () => {
      aliveRef.current = false;
      clearTimer();
    };
  }, [enabled, req, pollMs, run, clearTimer, key]);

  return { data, error, loading, refresh: run };
}
