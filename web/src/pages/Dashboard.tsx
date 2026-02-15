import React, { useEffect, useMemo, useRef, useState } from "react";
import StockChart from "@/components/charts/StockChart";
import { getSettings, getChartPrefs, setChartPrefs as persistChartPrefs, type ChartPrefs } from "@/lib/storage";

type SummaryState = {
  totalResults: number;
  activeProfiles: number;
  lastUpdated: string;
};

type ProfileSource = "legacy" | "gateway";

type WidgetId = "summary" | "crypto_index" | "top_movers";
type WidgetSize = "xl" | "lg" | "md" | "sm";
type CustomWidgetType = "kpi" | "note" | "table" | "chart";

type CustomWidget = {
  id: string;
  type: CustomWidgetType;
  title: string;
  config: Record<string, any>;
};

type WidgetDef = {
  id: WidgetId;
  title: string;
  size: WidgetSize;
};

const DEFAULT_SUMMARY_INTERVAL = 10 * 60 * 1000;
const WIDGET_KEY = "chartly.widgets.dashboard.v1";
const WIDGET_LAYOUT_KEY = "chartly.widgets.dashboard.layout.v1";
const CUSTOM_WIDGET_KEY = "chartly.widgets.dashboard.custom.v1";
const PROFILE_KEY = "chartly.widgets.dashboard.profile.v1";
const WIDGET_PREFS_KEY = "chartly.widgets.dashboard.prefs.v1";

const widgetCatalog: WidgetDef[] = [
  { id: "summary", title: "Executive Summary", size: "lg" },
  { id: "crypto_index", title: "Crypto Index", size: "lg" },
  { id: "top_movers", title: "Top Movers", size: "md" },
];
const widgetPresets: Record<string, { order: Array<{ id: WidgetId; size: WidgetSize }>; enabled: WidgetId[] }> = {
  overview: {
    order: [
      { id: "summary", size: "lg" },
      { id: "crypto_index", size: "lg" },
      { id: "top_movers", size: "md" },
    ],
    enabled: ["summary", "crypto_index", "top_movers"],
  },
  metrics: {
    order: [
      { id: "crypto_index", size: "xl" },
      { id: "summary", size: "md" },
      { id: "top_movers", size: "md" },
    ],
    enabled: ["crypto_index", "summary", "top_movers"],
  },
};

function nowIso() {
  return new Date().toISOString();
}

async function fetchJson(url: string, timeoutMs = 8000, signal?: AbortSignal): Promise<any> {
  const ctrl = new AbortController();
  const t = setTimeout(() => ctrl.abort(), timeoutMs);
  const outer = signal;
  if (outer) {
    if (outer.aborted) ctrl.abort();
    else outer.addEventListener("abort", () => ctrl.abort(), { once: true });
  }
  try {
    const res = await fetch(url, { signal: ctrl.signal });
    if (!res.ok) return null;
    return await res.json();
  } catch {
    return null;
  } finally {
    clearTimeout(t);
  }
}

function parseTime(s: string): number | null {
  if (!s) return null;
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : null;
}

function getGatewayTenant(): string {
  try {
    return (localStorage.getItem("chartly.usecase.tenant") || "").trim();
  } catch {
    return "";
  }
}

function gatewayHeaders(requestId: string): Record<string, string> {
  const headers: Record<string, string> = {
    accept: "application/json",
    "x-request-id": requestId,
    traceparent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
  };
  const tenant = getGatewayTenant();
  if (tenant) headers["x-chartly-tenant"] = tenant;
  return headers;
}

export default function Dashboard() {
  const settings = getSettings();
  const summaryInterval =
    typeof settings?.summaryCadenceMin === "number"
      ? Math.max(1, settings.summaryCadenceMin) * 60 * 1000
      : DEFAULT_SUMMARY_INTERVAL;
  const [summary, setSummary] = useState<SummaryState>({
    totalResults: 0,
    activeProfiles: 0,
    lastUpdated: nowIso()
  });
  const [summaryError, setSummaryError] = useState<string>("");
  const [chartPrefs, setChartPrefs] = useState<ChartPrefs>(() => getChartPrefs());
  const [activeSymbol, setActiveSymbol] = useState<string>("");
  const [widgetPrefs, setWidgetPrefs] = useState<Record<string, Partial<ChartPrefs>>>(() => {
    try {
      const raw = localStorage.getItem(WIDGET_PREFS_KEY);
      const parsed = raw ? JSON.parse(raw) : null;
      return parsed && typeof parsed === "object" ? parsed : {};
    } catch {
      return {};
    }
  });
  const [enabledWidgets, setEnabledWidgets] = useState<WidgetId[]>(() => {
    try {
      const raw = localStorage.getItem(WIDGET_KEY);
      const parsed = raw ? JSON.parse(raw) : null;
      const list = Array.isArray(parsed) ? parsed : [];
      const valid = list.filter((v: any) => v === "summary" || v === "crypto_index" || v === "top_movers");
      return valid.length ? valid : ["summary", "crypto_index", "top_movers"];
    } catch {
      return ["summary", "crypto_index", "top_movers"];
    }
  });
  const [layoutMode, setLayoutMode] = useState<boolean>(false);
  const [widgetLayout, setWidgetLayout] = useState<Array<{ id: WidgetId; size: WidgetSize }>>(() => {
    try {
      const raw = localStorage.getItem(WIDGET_LAYOUT_KEY);
      const parsed = raw ? JSON.parse(raw) : null;
      if (Array.isArray(parsed)) {
        const valid = parsed.filter((v: any) => widgetCatalog.some((w) => w.id === v?.id));
        if (valid.length) {
          return valid.map((v: any) => ({
            id: v.id as WidgetId,
            size: (v.size as WidgetSize) || widgetCatalog.find((w) => w.id === v.id)?.size || "md",
          }));
        }
      }
    } catch {
      // ignore
    }
    return widgetCatalog.map((w) => ({ id: w.id, size: w.size }));
  });
  const [indexSeries, setIndexSeries] = useState<{ t: number; v: number }[]>([]);
  const [symbolSeriesMap, setSymbolSeriesMap] = useState<Record<string, { t: number; v: number }[]>>({});
  const [moversRows, setMoversRows] = useState<any[]>([]);
  const lastUpdateRef = useRef<string>(nowIso());
  const [reportOptions, setReportOptions] = useState<Array<{ id: string; name?: string }>>([]);
  const [customWidgets, setCustomWidgets] = useState<CustomWidget[]>(() => {
    try {
      const raw = localStorage.getItem(CUSTOM_WIDGET_KEY);
      const parsed = raw ? JSON.parse(raw) : null;
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  });
  const [profiles, setProfiles] = useState<Array<{ id: string; name?: string }>>([]);
  const [profileSource, setProfileSource] = useState<ProfileSource>("legacy");
  const [selectedProfile, setSelectedProfile] = useState<string>(() => {
    try {
      return localStorage.getItem(PROFILE_KEY) || "all";
    } catch {
      return "all";
    }
  });
  const [resultsRows, setResultsRows] = useState<any[]>([]);
  const symbolProfiles = useMemo(
    () => buildSymbolProfiles(moversRows, symbolSeriesMap),
    [moversRows, symbolSeriesMap]
  );
  const visibleProfiles = useMemo(() => {
    if (symbolProfiles.length > 0) return symbolProfiles;
    if (profileSource === "gateway" && profiles.length > 0) return profiles;
    return profiles;
  }, [profileSource, profiles, symbolProfiles]);
  const selectedIsSymbolProfile = useMemo(
    () => selectedProfile !== "all" && symbolProfiles.some((p) => p.id === selectedProfile),
    [selectedProfile, symbolProfiles]
  );
  const selectedProfileLabel = useMemo(() => {
    if (selectedProfile === "all") return "All";
    const hit = visibleProfiles.find((p) => p.id === selectedProfile);
    return hit?.name || selectedProfile;
  }, [selectedProfile, visibleProfiles]);

  useEffect(() => {
    const refresh = async () => {
      setSummaryError("");
      const sum = await fetchJson("/api/summary");
      if (sum) {
        lastUpdateRef.current = sum.last_updated || nowIso();
        setSummary({
          totalResults: sum.total_results ?? 0,
          activeProfiles: sum.active_profiles ?? 0,
          lastUpdated: lastUpdateRef.current
        });
        return;
      }

      const profiles = await fetchJson("/api/profiles");
      const profileList = Array.isArray(profiles) ? profiles : profiles?.profiles ?? [];
      const activeProfiles = profileList.length;

      const agg = await fetchJson("/api/results/summary");
      let totalResults = agg?.total_results ?? 0;
      if (!totalResults) {
        const wall = await fetchJson("/api/reports/live-crypto-wall");
        totalResults = Array.isArray(wall?.rows) ? wall.rows.length : 0;
      }

      lastUpdateRef.current = nowIso();
      setSummary({
        totalResults,
        activeProfiles,
        lastUpdated: lastUpdateRef.current
      });
      if (!profiles && !agg) {
        setSummaryError("Summary unavailable");
      }
    };

    refresh();
    const t = setInterval(refresh, summaryInterval);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    localStorage.setItem(WIDGET_KEY, JSON.stringify(enabledWidgets));
  }, [enabledWidgets]);

  useEffect(() => {
    persistChartPrefs(chartPrefs);
  }, [chartPrefs]);

  useEffect(() => {
    localStorage.setItem(WIDGET_PREFS_KEY, JSON.stringify(widgetPrefs));
  }, [widgetPrefs]);

  useEffect(() => {
    localStorage.setItem(WIDGET_LAYOUT_KEY, JSON.stringify(widgetLayout));
  }, [widgetLayout]);

  useEffect(() => {
    localStorage.setItem(CUSTOM_WIDGET_KEY, JSON.stringify(customWidgets));
  }, [customWidgets]);

  useEffect(() => {
    try {
      localStorage.setItem(PROFILE_KEY, selectedProfile);
    } catch {
      // ignore
    }
  }, [selectedProfile]);

  useEffect(() => {
    let mounted = true;
    let t: number | undefined;
    const refresh = async () => {
      const idx = await fetchJson("/api/reports/crypto-index");
      if (mounted) {
        const s = Array.isArray(idx?.series) ? idx.series[0] : null;
        const pts = Array.isArray(s?.points) ? s.points : [];
        const normalized = pts
          .map((p: any) => {
            const tt = parseTime(p?.t);
            const vv = typeof p?.y === "number" ? p.y : typeof p?.v === "number" ? p.v : null;
            if (tt == null || vv == null) return null;
            return { t: tt, v: vv };
          })
          .filter(Boolean) as { t: number; v: number }[];
        normalized.sort((a, b) => a.t - b.t);
        setIndexSeries(normalized.slice(-600));
      }
      const movers = await fetchJson("/api/crypto/top?limit=10");
      if (mounted && Array.isArray(movers)) {
        setMoversRows(movers);
        setSymbolSeriesMap((prev) => appendSymbolSeries(prev, movers, 600));
      }
      t = window.setTimeout(refresh, 5000);
    };
    refresh();
    return () => {
      mounted = false;
      if (t) window.clearTimeout(t);
    };
  }, []);

  useEffect(() => {
    let stopped = false;
    const load = async () => {
      try {
        const gw = await fetch("/v1/profiles", { headers: gatewayHeaders(`dash-profiles-${Date.now()}`) });
        if (gw.ok) {
          const json = await gw.json();
          const gwList = Array.isArray(json?.profiles) ? json.profiles : Array.isArray(json) ? json : [];
          const normalized: Array<{ id: string; name?: string }> = gwList
            .map((p: any) => ({ id: String(p?.id || "").trim(), name: String(p?.id || "").trim() }))
            .filter((p: { id: string }) => p.id.length > 0);
          if (!stopped && normalized.length > 0) {
            normalized.sort((a, b) => a.id.localeCompare(b.id));
            setProfiles(normalized);
            setProfileSource("gateway");
            return;
          }
        }
      } catch {
        // fall through to legacy
      }

      fetch("/api/profiles")
        .then((r) => (r.ok ? r.json() : []))
        .then((list) => {
          if (stopped) return;
          const arr = Array.isArray(list) ? list : list?.profiles ?? [];
          setProfiles(arr.map((p: any) => ({ id: p.id, name: p.name || p.id })));
          setProfileSource("legacy");
        })
        .catch(() => {
          if (!stopped) {
            setProfiles([]);
            setProfileSource("legacy");
          }
        });
    };

    load();
    return () => {
      stopped = true;
    };
  }, []);

  useEffect(() => {
    if (selectedProfile === "all") return;
    if (visibleProfiles.some((p) => p.id === selectedProfile)) return;
    if (visibleProfiles.length > 0) {
      setSelectedProfile(visibleProfiles[0].id);
      return;
    }
    setSelectedProfile("all");
  }, [visibleProfiles, selectedProfile]);

  useEffect(() => {
    if (selectedProfile === "all") {
      setActiveSymbol("");
      return;
    }
    if (selectedIsSymbolProfile) {
      setActiveSymbol((prev) => (prev === selectedProfile ? prev : selectedProfile));
      return;
    }
    setActiveSymbol("");
  }, [selectedProfile, selectedIsSymbolProfile]);

  useEffect(() => {
    let mounted = true;
    let t: number | undefined;
    const refresh = async () => {
      if (profileSource === "gateway") {
        if (selectedProfile === "all") {
          if (mounted) setResultsRows([]);
        } else {
          try {
            const res = await fetch("/v1/events/query", {
              method: "POST",
              headers: {
                ...gatewayHeaders(`dash-query-${Date.now()}`),
                "content-type": "application/json",
              },
              body: JSON.stringify({ profile_id: selectedProfile }),
            });
            if (res.ok) {
              const payload = await res.json();
              const rows = Array.isArray(payload?.events) ? payload.events : [];
              if (mounted) setResultsRows(rows);
            } else if (mounted) {
              setResultsRows([]);
            }
          } catch {
            if (mounted) setResultsRows([]);
          }
        }
      } else {
        if (selectedIsSymbolProfile) {
          if (mounted) setResultsRows([]);
        } else {
          const params = new URLSearchParams();
          params.set("limit", "50");
          if (selectedProfile && selectedProfile !== "all") {
            params.set("profile_id", selectedProfile);
          }
          const data = await fetchJson(`/api/results?${params.toString()}`);
          if (mounted && Array.isArray(data)) {
            setResultsRows(data);
          }
        }
      }
      t = window.setTimeout(refresh, 5000);
    };
    refresh();
    return () => {
      mounted = false;
      if (t) window.clearTimeout(t);
    };
  }, [selectedProfile, profileSource, selectedIsSymbolProfile]);

  useEffect(() => {
    fetch("/api/reports")
      .then((r) => (r.ok ? r.json() : []))
      .then((list) => {
        const arr = Array.isArray(list) ? list : [];
        setReportOptions(arr.map((it: any) => ({ id: it.id, name: it.name || it.id })));
      })
      .catch(() => setReportOptions([]));
  }, []);

  const widgetToggles = useMemo(
    () => [
      { id: "summary" as WidgetId, label: "Summary" },
      { id: "crypto_index" as WidgetId, label: "Crypto Index" },
      { id: "top_movers" as WidgetId, label: "Top Movers" }
    ],
    []
  );

  const orderedWidgets = useMemo(() => {
    const order = widgetLayout.map((w) => w.id);
    const missing = widgetCatalog.filter((w) => !order.includes(w.id)).map((w) => w.id);
    return order.concat(missing);
  }, [widgetLayout]);

  const activeWidgetCount = enabledWidgets.length + customWidgets.length;
  const displaySummary = useMemo(() => {
    if (!selectedProfile || selectedProfile === "all") return summary;
    if (!resultsRows.length) return summary;
    let latest = 0;
    for (const r of resultsRows) {
      const ts = parseTime(r?.timestamp || r?.ts || "");
      if (ts != null && ts > latest) latest = ts;
    }
    return {
      totalResults: resultsRows.length,
      activeProfiles: 1,
      lastUpdated: latest ? new Date(latest).toISOString() : summary.lastUpdated,
    };
  }, [selectedProfile, resultsRows, summary]);

  const profileSeries = useMemo(() => buildBestResultsSeries(resultsRows, 600), [resultsRows]);
  const activeSymbolSeries = useMemo(
    () => (activeSymbol ? symbolSeriesMap[activeSymbol] || [] : []),
    [activeSymbol, symbolSeriesMap]
  );
  const chartSeries = useMemo(() => {
    if (activeSymbolSeries.length > 0) return activeSymbolSeries;
    if (selectedProfile !== "all" && profileSeries.length > 0) return profileSeries;
    return indexSeries;
  }, [activeSymbolSeries, selectedProfile, profileSeries, indexSeries]);
  const chartTitle = useMemo(() => {
    if (activeSymbolSeries.length > 0 && activeSymbol) return `${activeSymbol} Trend`;
    if (selectedProfile !== "all" && profileSeries.length > 0) return `${selectedProfile} Trend`;
    return "Crypto Index";
  }, [activeSymbolSeries.length, activeSymbol, selectedProfile, profileSeries.length]);
  const chartYLabel = useMemo(() => {
    if (activeSymbolSeries.length > 0 && activeSymbol) return `${activeSymbol} Price`;
    if (selectedProfile !== "all" && profileSeries.length > 0) return "Value";
    return "Index";
  }, [activeSymbolSeries.length, activeSymbol, selectedProfile, profileSeries.length]);

  const sizeFor = (id: WidgetId): WidgetSize =>
    widgetLayout.find((w) => w.id === id)?.size || widgetCatalog.find((w) => w.id === id)?.size || "md";

  const moveWidget = (id: WidgetId, dir: -1 | 1) => {
    setWidgetLayout((prev) => {
      const idx = prev.findIndex((w) => w.id === id);
      if (idx < 0) return prev;
      const next = prev.slice();
      const swap = idx + dir;
      if (swap < 0 || swap >= next.length) return prev;
      const tmp = next[idx];
      next[idx] = next[swap];
      next[swap] = tmp;
      return next;
    });
  };

  const setWidgetSize = (id: WidgetId, size: WidgetSize) => {
    setWidgetLayout((prev) => prev.map((w) => (w.id === id ? { ...w, size } : w)));
  };

  const renderWidget = (id: WidgetId) => {
    if (!enabledWidgets.includes(id)) return null;

    if (id === "summary") {
      return (
        <Widget
          key={id}
          size={sizeFor("summary")}
          title="Executive Summary"
          layoutMode={layoutMode}
          onMoveUp={() => moveWidget("summary", -1)}
          onMoveDown={() => moveWidget("summary", 1)}
          onResize={(s) => setWidgetSize("summary", s)}
        >
          <div style={{ display: "grid", gridTemplateColumns: "repeat(3, minmax(0,1fr))", gap: 12 }}>
            <Card title="Total Results" value={displaySummary.totalResults} />
            <Card title="Active Profiles" value={displaySummary.activeProfiles} />
            <Card title="Last Updated" value={displaySummary.lastUpdated} />
          </div>
          {summaryError ? <div style={{ marginTop: 8, fontSize: 12, opacity: 0.7 }}>{summaryError}</div> : null}
        </Widget>
      );
    }

    if (id === "crypto_index") {
      const prefs = { ...chartPrefs, ...(widgetPrefs[id] || {}) };
      return (
        <Widget
          key={id}
          size={sizeFor("crypto_index")}
          title={chartTitle}
          layoutMode={layoutMode}
          onMoveUp={() => moveWidget("crypto_index", -1)}
          onMoveDown={() => moveWidget("crypto_index", 1)}
          onResize={(s) => setWidgetSize("crypto_index", s)}
        >
          <StockChart
            data={chartSeries}
            color="#4ea1ff"
            showAxisLabels={prefs.showAxisLabels}
            labelDensity={prefs.labelDensity}
            tooltipDetail={prefs.tooltipDetail}
            xLabel="Time"
            yLabel={chartYLabel}
          />
          {layoutMode ? (
            <div style={{ borderTop: "1px solid #1f2228", marginTop: 8, paddingTop: 8, display: "grid", gap: 6 }}>
              <div style={{ fontSize: 12, opacity: 0.75, display: "flex", justifyContent: "space-between" }}>
                <span>Widget prefs</span>
                <button
                  onClick={() =>
                    setWidgetPrefs((prev) => {
                      const next = { ...prev };
                      if (next[id]) delete next[id];
                      else next[id] = { ...chartPrefs };
                      return next;
                    })
                  }
                  style={miniBtn()}
                >
                  {widgetPrefs[id] ? "Use global" : "Customize"}
                </button>
              </div>
              {widgetPrefs[id] ? (
                <>
                  <label style={{ display: "grid", gap: 4 }}>
                    <span style={{ fontSize: 12, opacity: 0.7 }}>Axis labels</span>
                    <select
                      value={prefs.showAxisLabels ? "on" : "off"}
                      onChange={(e) =>
                        setWidgetPrefs((prev) => ({
                          ...prev,
                          [id]: { ...(prev[id] || chartPrefs), showAxisLabels: e.target.value === "on" },
                        }))
                      }
                      style={input()}
                    >
                      <option value="on">On</option>
                      <option value="off">Off</option>
                    </select>
                  </label>
                  <label style={{ display: "grid", gap: 4 }}>
                    <span style={{ fontSize: 12, opacity: 0.7 }}>Label density</span>
                    <select
                      value={prefs.labelDensity}
                      onChange={(e) =>
                        setWidgetPrefs((prev) => ({
                          ...prev,
                          [id]: { ...(prev[id] || chartPrefs), labelDensity: e.target.value as ChartPrefs["labelDensity"] },
                        }))
                      }
                      style={input()}
                    >
                      <option value="full">Full</option>
                      <option value="sparse">Sparse</option>
                      <option value="none">None</option>
                    </select>
                  </label>
                  <label style={{ display: "grid", gap: 4 }}>
                    <span style={{ fontSize: 12, opacity: 0.7 }}>Tooltip detail</span>
                    <select
                      value={prefs.tooltipDetail}
                      onChange={(e) =>
                        setWidgetPrefs((prev) => ({
                          ...prev,
                          [id]: { ...(prev[id] || chartPrefs), tooltipDetail: e.target.value as ChartPrefs["tooltipDetail"] },
                        }))
                      }
                      style={input()}
                    >
                      <option value="minimal">Minimal</option>
                      <option value="full">Full</option>
                      <option value="full_ts">Full + timestamp</option>
                    </select>
                  </label>
                </>
              ) : null}
            </div>
          ) : null}
        </Widget>
      );
    }

    if (id === "top_movers") {
      const useResults = selectedProfile !== "all" && resultsRows.length > 0;
      const prefs = { ...chartPrefs, ...(widgetPrefs[id] || {}) };
      const moversLimit = Math.max(1, prefs.maxItems || 10);
      const filteredMovers =
        prefs.clickBehavior === "filter" && activeSymbol
          ? moversRows.filter((r) => r?.symbol === activeSymbol)
          : moversRows;
      return (
        <Widget
          key={id}
          size={sizeFor("top_movers")}
          title={useResults ? "Recent Results" : "Top Movers"}
          layoutMode={layoutMode}
          onMoveUp={() => moveWidget("top_movers", -1)}
          onMoveDown={() => moveWidget("top_movers", 1)}
          onResize={(s) => setWidgetSize("top_movers", s)}
        >
          {useResults ? (
            <Table
              rows={resultsRows.slice(0, 10).map((r) => ({
                profile_id: r?.profile_id || r?.profileId || r?.profile || "unknown",
                timestamp: r?.timestamp || r?.ts || "",
                summary: formatResultSummary(r),
              }))}
              columns={[
                { key: "profile_id", label: "Profile" },
                { key: "timestamp", label: "Timestamp" },
                { key: "summary", label: "Summary" },
              ]}
            />
          ) : (
            <Table
              rows={filteredMovers.slice(0, moversLimit)}
              columns={[
                { key: "symbol", label: "Symbol" },
                { key: "pct_change", label: "% Change" },
                { key: "price", label: "Last" },
              ]}
              onRowClick={
                prefs.clickBehavior === "filter"
                  ? (row) => {
                      const sym = String(row?.symbol || "").trim();
                      if (!sym) return;
                      setActiveSymbol((prev) => (prev === sym ? "" : sym));
                    }
                  : undefined
              }
            />
          )}
          {layoutMode ? (
            <div style={{ borderTop: "1px solid #1f2228", marginTop: 8, paddingTop: 8, display: "grid", gap: 6 }}>
              <div style={{ fontSize: 12, opacity: 0.75, display: "flex", justifyContent: "space-between" }}>
                <span>Widget prefs</span>
                <button
                  onClick={() =>
                    setWidgetPrefs((prev) => {
                      const next = { ...prev };
                      if (next[id]) delete next[id];
                      else next[id] = { ...chartPrefs };
                      return next;
                    })
                  }
                  style={miniBtn()}
                >
                  {widgetPrefs[id] ? "Use global" : "Customize"}
                </button>
              </div>
              {widgetPrefs[id] ? (
                <>
                  <label style={{ display: "grid", gap: 4 }}>
                    <span style={{ fontSize: 12, opacity: 0.7 }}>Max items</span>
                    <input
                      type="number"
                      value={prefs.maxItems}
                      onChange={(e) =>
                        setWidgetPrefs((prev) => ({
                          ...prev,
                          [id]: { ...(prev[id] || chartPrefs), maxItems: Math.max(1, Number(e.target.value) || 1) },
                        }))
                      }
                      style={input()}
                    />
                  </label>
                  <label style={{ display: "grid", gap: 4 }}>
                    <span style={{ fontSize: 12, opacity: 0.7 }}>Click behavior</span>
                    <select
                      value={prefs.clickBehavior}
                      onChange={(e) =>
                        setWidgetPrefs((prev) => ({
                          ...prev,
                          [id]: { ...(prev[id] || chartPrefs), clickBehavior: e.target.value as ChartPrefs["clickBehavior"] },
                        }))
                      }
                      style={input()}
                    >
                      <option value="none">None</option>
                      <option value="focus">Focus</option>
                      <option value="filter">Filter page</option>
                    </select>
                  </label>
                </>
              ) : null}
              {prefs.clickBehavior === "filter" ? (
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", fontSize: 12 }}>
                  <span style={{ opacity: 0.7 }}>Filter: {activeSymbol || "none"}</span>
                  {activeSymbol ? (
                    <button onClick={() => setActiveSymbol("")} style={miniBtn()}>
                      Clear
                    </button>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </Widget>
      );
    }

    return null;
  };

  const applyPreset = (preset: keyof typeof widgetPresets) => {
    const next = widgetPresets[preset];
    setEnabledWidgets(next.enabled);
    setWidgetLayout(next.order);
  };

  const resetLayout = () => {
    setEnabledWidgets(["summary", "crypto_index", "top_movers"]);
    setWidgetLayout(widgetCatalog.map((w) => ({ id: w.id, size: w.size })));
  };

  const exportLayout = async () => {
    const payload = {
      enabledWidgets,
      widgetLayout,
      customWidgets,
    };
    const text = JSON.stringify(payload);
    if (navigator?.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      window.prompt("Copy layout JSON", text);
    }
  };

  const importLayout = () => {
    const raw = window.prompt("Paste layout JSON");
    if (!raw) return;
    try {
      const parsed = JSON.parse(raw);
      const enabled = Array.isArray(parsed?.enabledWidgets)
        ? parsed.enabledWidgets.filter((v: any) => widgetCatalog.some((w) => w.id === v))
        : [];
      const layout = Array.isArray(parsed?.widgetLayout)
        ? parsed.widgetLayout
            .filter((v: any) => widgetCatalog.some((w) => w.id === v?.id))
            .map((v: any) => ({
              id: v.id as WidgetId,
              size: (v.size as WidgetSize) || widgetCatalog.find((w) => w.id === v.id)?.size || "md",
            }))
        : [];
      const custom = Array.isArray(parsed?.customWidgets) ? parsed.customWidgets : [];
      if (enabled.length) setEnabledWidgets(enabled);
      if (layout.length) setWidgetLayout(layout);
      if (custom.length) setCustomWidgets(custom);
    } catch {
      // ignore
    }
  };

  const addCustomWidget = (type: CustomWidgetType) => {
    const id = `custom_${Date.now()}_${Math.random().toString(16).slice(2, 8)}`;
    const base: CustomWidget = {
      id,
      type,
      title:
        type === "kpi"
          ? "KPI"
          : type === "note"
          ? "Note"
          : type === "table"
          ? "Table"
          : "Chart",
      config: {},
    };
    if (type === "kpi") {
      base.config = { source: "summary", field: "totalResults", manualValue: "0", delta: "", size: "md" };
    }
    if (type === "note") {
      base.config = { text: "Add notes here.", size: "md" };
    }
    if (type === "table") {
      base.config = { source: "top_movers", limit: 10, size: "md" };
    }
    if (type === "chart") {
      base.config =
        selectedProfile !== "all"
          ? { source: "results", fieldPath: "data.c", groupBy: "", groupValue: "", agg: "last", limit: 120, size: "lg" }
          : { source: "crypto_index", size: "lg" };
    }
    setCustomWidgets((prev) => prev.concat([base]));
  };

  const updateCustomWidget = (id: string, next: Partial<CustomWidget>) => {
    setCustomWidgets((prev) => prev.map((w) => (w.id === id ? { ...w, ...next } : w)));
  };

  const updateCustomConfig = (id: string, patch: Record<string, any>) => {
    setCustomWidgets((prev) =>
      prev.map((w) => (w.id === id ? { ...w, config: { ...w.config, ...patch } } : w))
    );
  };

  const removeCustomWidget = (id: string) => {
    setCustomWidgets((prev) => prev.filter((w) => w.id !== id));
  };

  const duplicateCustomWidget = (id: string) => {
    setCustomWidgets((prev) => {
      const src = prev.find((w) => w.id === id);
      if (!src) return prev;
      const next: CustomWidget = {
        ...src,
        id: `custom_${Date.now()}_${Math.random().toString(16).slice(2, 8)}`,
        title: `${src.title} Copy`,
      };
      return prev.concat([next]);
    });
  };

  const moveCustomWidget = (id: string, dir: -1 | 1) => {
    setCustomWidgets((prev) => {
      const idx = prev.findIndex((w) => w.id === id);
      if (idx < 0) return prev;
      const swap = idx + dir;
      if (swap < 0 || swap >= prev.length) return prev;
      const next = prev.slice();
      const tmp = next[idx];
      next[idx] = next[swap];
      next[swap] = tmp;
      return next;
    });
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, flexWrap: "wrap" }}>
        <div style={{ fontSize: 18, fontWeight: 800 }}>Dashboard</div>
        <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <div style={{ fontSize: 12, opacity: 0.7 }}>Widgets: {activeWidgetCount}</div>
          <div style={{ fontSize: 12, opacity: 0.7 }}>Profile: {selectedProfileLabel}</div>
          {selectedProfile !== "all" ? <div style={{ fontSize: 12, opacity: 0.7 }}>Results: {resultsRows.length}</div> : null}
          <div style={{ display: "flex", gap: 6, alignItems: "center" }}>
            <label style={{ fontSize: 12, opacity: 0.7 }}>Profile</label>
            <select value={selectedProfile} onChange={(e) => setSelectedProfile(e.target.value)} style={input()}>
              <option value="all">All profiles</option>
              {visibleProfiles.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name || p.id}
                </option>
              ))}
            </select>
          </div>
        </div>
      </div>

      <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
        {widgetToggles.map((w) => {
          const enabled = enabledWidgets.includes(w.id);
          return (
            <button
              key={w.id}
              onClick={() =>
                setEnabledWidgets((prev) => {
                  if (enabled) return prev.filter((id) => id !== w.id);
                  return prev.concat([w.id]);
                })
              }
              style={{
                padding: "6px 10px",
                borderRadius: 8,
                border: "1px solid #1f2228",
                background: enabled ? "#1f2a37" : "#0f1115",
                color: enabled ? "#e5e7eb" : "#9ca3af",
                cursor: "pointer"
              }}
            >
              {enabled ? "Hide" : "Show"} {w.label}
            </button>
          );
        })}
        <button
          onClick={() => setLayoutMode((v) => !v)}
          style={{
            padding: "6px 10px",
            borderRadius: 8,
            border: "1px solid #1f2228",
            background: layoutMode ? "#233044" : "#0f1115",
            color: layoutMode ? "#e5e7eb" : "#9ca3af",
            cursor: "pointer"
          }}
        >
          {layoutMode ? "Exit Layout" : "Edit Layout"}
        </button>
        <button
          onClick={() => applyPreset("overview")}
          style={{
            padding: "6px 10px",
            borderRadius: 8,
            border: "1px solid #1f2228",
            background: "#0f1115",
            color: "#9ca3af",
            cursor: "pointer"
          }}
        >
          Preset: Overview
        </button>
        <button
          onClick={() => applyPreset("metrics")}
          style={{
            padding: "6px 10px",
            borderRadius: 8,
            border: "1px solid #1f2228",
            background: "#0f1115",
            color: "#9ca3af",
            cursor: "pointer"
          }}
        >
          Preset: Metrics
        </button>
        <button
          onClick={resetLayout}
          style={{
            padding: "6px 10px",
            borderRadius: 8,
            border: "1px solid #1f2228",
            background: "#0f1115",
            color: "#9ca3af",
            cursor: "pointer"
          }}
        >
          Reset Layout
        </button>
        <button
          onClick={exportLayout}
          style={{
            padding: "6px 10px",
            borderRadius: 8,
            border: "1px solid #1f2228",
            background: "#0f1115",
            color: "#9ca3af",
            cursor: "pointer"
          }}
        >
          Copy Layout
        </button>
        <button
          onClick={importLayout}
          style={{
            padding: "6px 10px",
            borderRadius: 8,
            border: "1px solid #1f2228",
            background: "#0f1115",
            color: "#9ca3af",
            cursor: "pointer"
          }}
        >
          Import Layout
        </button>
        <AddWidgetMenu onAdd={addCustomWidget} />
      </div>

      <div style={{ border: "1px solid #1f2228", borderRadius: 8, padding: 12, background: "#0f1115" }}>
        <div style={{ fontSize: 12, fontWeight: 700, opacity: 0.8, marginBottom: 8 }}>Chart Preferences</div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))", gap: 10 }}>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 12, opacity: 0.7 }}>Axis labels</span>
            <select
              value={chartPrefs.showAxisLabels ? "on" : "off"}
              onChange={(e) => setChartPrefs((p) => ({ ...p, showAxisLabels: e.target.value === "on" }))}
              style={input()}
            >
              <option value="on">On</option>
              <option value="off">Off</option>
            </select>
          </label>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 12, opacity: 0.7 }}>Label density</span>
            <select
              value={chartPrefs.labelDensity}
              onChange={(e) =>
                setChartPrefs((p) => ({ ...p, labelDensity: e.target.value as ChartPrefs["labelDensity"] }))
              }
              style={input()}
            >
              <option value="full">Full</option>
              <option value="sparse">Sparse</option>
              <option value="none">None</option>
            </select>
          </label>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 12, opacity: 0.7 }}>Max items</span>
            <input
              type="number"
              value={chartPrefs.maxItems}
              onChange={(e) => setChartPrefs((p) => ({ ...p, maxItems: Math.max(1, Number(e.target.value) || 1) }))}
              style={input()}
            />
          </label>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 12, opacity: 0.7 }}>Tooltip detail</span>
            <select
              value={chartPrefs.tooltipDetail}
              onChange={(e) =>
                setChartPrefs((p) => ({ ...p, tooltipDetail: e.target.value as ChartPrefs["tooltipDetail"] }))
              }
              style={input()}
            >
              <option value="minimal">Minimal</option>
              <option value="full">Full</option>
              <option value="full_ts">Full + timestamp</option>
            </select>
          </label>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 12, opacity: 0.7 }}>Legend</span>
            <select
              value={chartPrefs.showLegend ? "on" : "off"}
              onChange={(e) => setChartPrefs((p) => ({ ...p, showLegend: e.target.value === "on" }))}
              style={input()}
            >
              <option value="off">Off</option>
              <option value="on">On</option>
            </select>
          </label>
          <label style={{ display: "grid", gap: 4 }}>
            <span style={{ fontSize: 12, opacity: 0.7 }}>Click behavior</span>
            <select
              value={chartPrefs.clickBehavior}
              onChange={(e) =>
                setChartPrefs((p) => ({ ...p, clickBehavior: e.target.value as ChartPrefs["clickBehavior"] }))
              }
              style={input()}
            >
              <option value="none">None</option>
              <option value="focus">Focus</option>
              <option value="filter">Filter page</option>
            </select>
          </label>
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(12, minmax(0,1fr))", gap: 12 }}>
        {orderedWidgets.map((id) => renderWidget(id))}
        {customWidgets.map((w) => (
          <CustomDashboardWidget
            key={w.id}
            widget={w}
            layoutMode={layoutMode}
            refreshMs={5000}
            summary={summary}
            moversRows={moversRows}
            resultsRows={resultsRows}
            indexSeries={indexSeries}
            reportOptions={reportOptions}
            chartPrefs={chartPrefs}
            activeSymbol={activeSymbol}
            onActiveSymbolChange={setActiveSymbol}
            onRemove={() => removeCustomWidget(w.id)}
            onDuplicate={() => duplicateCustomWidget(w.id)}
            onChange={(next) => updateCustomWidget(w.id, next)}
            onConfig={(patch) => updateCustomConfig(w.id, patch)}
            onMoveUp={() => moveCustomWidget(w.id, -1)}
            onMoveDown={() => moveCustomWidget(w.id, 1)}
          />
        ))}
      </div>
    </div>
  );
}

function Widget({
  title,
  subtitle,
  size,
  layoutMode,
  onMoveUp,
  onMoveDown,
  onResize,
  children,
}: {
  title: string;
  subtitle?: string;
  size: WidgetSize;
  layoutMode?: boolean;
  onMoveUp?: () => void;
  onMoveDown?: () => void;
  onResize?: (size: WidgetSize) => void;
  children: React.ReactNode;
}) {
  const span = size === "xl" ? "span 12" : size === "lg" ? "span 8" : size === "md" ? "span 4" : "span 3";
  return (
    <div style={{ gridColumn: span, padding: 14, borderRadius: 12, border: "1px solid #1f2a37", background: "#0f141c" }}>
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", marginBottom: 10 }}>
        <div>
          <div style={{ fontWeight: 800 }}>{title}</div>
          {subtitle ? <div style={{ fontSize: 12, opacity: 0.65 }}>{subtitle}</div> : null}
        </div>
        {layoutMode ? (
          <div style={{ display: "flex", gap: 6 }}>
            <button onClick={onMoveUp} style={miniBtn()}>
              Up
            </button>
            <button onClick={onMoveDown} style={miniBtn()}>
              Down
            </button>
            <select
              value={size}
              onChange={(e) => onResize?.(e.target.value as WidgetSize)}
              style={{ ...input(), padding: "4px 6px", fontSize: 12 }}
            >
              <option value="sm">sm</option>
              <option value="md">md</option>
              <option value="lg">lg</option>
              <option value="xl">xl</option>
            </select>
          </div>
        ) : null}
      </div>
      {children}
    </div>
  );
}

function CustomDashboardWidget({
  widget,
  layoutMode,
  refreshMs,
  summary,
  moversRows,
  resultsRows,
  indexSeries,
  reportOptions,
  chartPrefs,
  activeSymbol,
  onActiveSymbolChange,
  onRemove,
  onDuplicate,
  onChange,
  onConfig,
  onMoveUp,
  onMoveDown,
}: {
  widget: CustomWidget;
  layoutMode: boolean;
  refreshMs: number;
  summary: SummaryState;
  moversRows: any[];
  resultsRows: any[];
  indexSeries: { t: number; v: number }[];
  reportOptions: Array<{ id: string; name?: string }>;
  chartPrefs: ChartPrefs;
  activeSymbol: string;
  onActiveSymbolChange: (next: string) => void;
  onRemove: () => void;
  onDuplicate: () => void;
  onChange: (next: Partial<CustomWidget>) => void;
  onConfig: (patch: Record<string, any>) => void;
  onMoveUp: () => void;
  onMoveDown: () => void;
}) {
  const [expanded, setExpanded] = useState<boolean>(false);
  const useCustomPrefs = Boolean(widget.config.usePrefs);
  const prefs = useCustomPrefs ? { ...chartPrefs, ...(widget.config.prefs || {}) } : chartPrefs;

  const updatePrefs = (patch: Partial<ChartPrefs>) => {
    onConfig({ usePrefs: true, prefs: { ...(widget.config.prefs || {}), ...patch } });
  };
  let body: React.ReactNode = null;
  if (widget.type === "kpi") {
    const val =
      widget.config.source === "summary"
        ? (summary as any)[widget.config.field] ?? 0
        : widget.config.manualValue ?? "";
    body = (
      <div style={{ display: "grid", gap: 8 }}>
        <div style={{ fontSize: 28, fontWeight: 800 }}>{val}</div>
        {widget.config.delta ? <div style={{ fontSize: 12, opacity: 0.7 }}>Delta {widget.config.delta}</div> : null}
        <div style={{ display: "grid", gap: 6 }}>
          <label style={{ fontSize: 12, opacity: 0.7 }}>Source</label>
          <select
            value={widget.config.source || "summary"}
            onChange={(e) => onConfig({ source: e.target.value })}
            style={input()}
          >
            <option value="summary">Summary</option>
            <option value="manual">Manual</option>
          </select>
          {widget.config.source === "summary" ? (
            <>
              <label style={{ fontSize: 12, opacity: 0.7 }}>Field</label>
              <select
                value={widget.config.field || "totalResults"}
                onChange={(e) => onConfig({ field: e.target.value })}
                style={input()}
              >
                <option value="totalResults">totalResults</option>
                <option value="activeProfiles">activeProfiles</option>
                <option value="lastUpdated">lastUpdated</option>
              </select>
            </>
          ) : (
            <>
              <label style={{ fontSize: 12, opacity: 0.7 }}>Manual Value</label>
              <input
                value={widget.config.manualValue || ""}
                onChange={(e) => onConfig({ manualValue: e.target.value })}
                style={input()}
              />
            </>
          )}
          {expanded ? (
            <>
              <label style={{ fontSize: 12, opacity: 0.7 }}>Delta</label>
              <input
                value={widget.config.delta || ""}
                onChange={(e) => onConfig({ delta: e.target.value })}
                style={input()}
              />
            </>
          ) : null}
        </div>
      </div>
    );
  }

  if (widget.type === "note") {
    body = (
      <textarea
        value={widget.config.text || ""}
        onChange={(e) => onConfig({ text: e.target.value })}
        rows={expanded ? 8 : 4}
        style={{ ...input(), minHeight: expanded ? 160 : 90 }}
      />
    );
  }

  if (widget.type === "table") {
    const source = widget.config.source || "top_movers";
    const limit = Number(widget.config.limit) || 10;
    const resultRows = resultsRows.map((r) => ({
      profile_id: r?.profile_id || r?.profileId || r?.profile || "unknown",
      timestamp: r?.timestamp || r?.ts || "",
      summary: formatResultSummary(r),
    }));
    const baseRows = source === "results" ? resultRows : moversRows;
    const rows =
      prefs.clickBehavior === "filter" && activeSymbol && source !== "results"
        ? baseRows.filter((r) => r?.symbol === activeSymbol)
        : baseRows;
    const maxRows = Math.min(limit, prefs.maxItems || limit);
    body = (
      <div style={{ display: "grid", gap: 8 }}>
        {expanded ? (
          <>
            <label style={{ fontSize: 12, opacity: 0.7 }}>Source</label>
            <select value={source} onChange={(e) => onConfig({ source: e.target.value })} style={input()}>
              <option value="top_movers">Top Movers</option>
              <option value="results">Results Feed</option>
            </select>
          </>
        ) : null}
        {expanded ? (
          <>
            <label style={{ fontSize: 12, opacity: 0.7 }}>Rows</label>
            <input
              type="number"
              value={widget.config.limit || 10}
              onChange={(e) => onConfig({ limit: Math.max(1, Number(e.target.value) || 10) })}
              style={input()}
            />
          </>
        ) : null}
        <Table
          rows={rows.slice(0, maxRows)}
          columns={
            source === "results"
              ? [
                  { key: "profile_id", label: "Profile" },
                  { key: "timestamp", label: "Timestamp" },
                  { key: "summary", label: "Summary" },
                ]
              : [
                  { key: "symbol", label: "Symbol" },
                  { key: "pct_change", label: "% Change" },
                  { key: "price", label: "Last" },
                ]
          }
          onRowClick={
            prefs.clickBehavior === "filter" && source !== "results"
              ? (row) => {
                  const sym = String(row?.symbol || "").trim();
                  if (!sym) return;
                  onActiveSymbolChange(activeSymbol === sym ? "" : sym);
                }
              : undefined
          }
        />
      </div>
    );
  }

  if (widget.type === "chart") {
    const source = widget.config.source || "crypto_index";
    const fieldPath = widget.config.fieldPath || "data.c";
    const groupBy = widget.config.groupBy || "";
    const groupValue = widget.config.groupValue || "";
    const agg = widget.config.agg || "last";
    const limit = Math.max(10, Number(widget.config.limit) || 120);
    const groupOptions = buildGroupOptions(resultsRows, groupBy);
    body = (
      <div style={{ display: "grid", gap: 8 }}>
        {expanded ? (
          <>
            <label style={{ fontSize: 12, opacity: 0.7 }}>Source</label>
            <select value={source} onChange={(e) => onConfig({ source: e.target.value })} style={input()}>
              <option value="crypto_index">Crypto Index</option>
              <option value="results">Results Sparkline</option>
              <option value="report">Report</option>
            </select>
          </>
        ) : null}
        {source === "results" ? (
          <>
            {expanded ? (
              <>
                <label style={{ fontSize: 12, opacity: 0.7 }}>Field Path</label>
                <input value={fieldPath} onChange={(e) => onConfig({ fieldPath: e.target.value })} style={input()} />
                <label style={{ fontSize: 12, opacity: 0.7 }}>Group By (optional)</label>
                <input value={groupBy} onChange={(e) => onConfig({ groupBy: e.target.value })} style={input()} />
                {groupOptions.length ? (
                  <>
                    <label style={{ fontSize: 12, opacity: 0.7 }}>Group Value</label>
                    <select value={groupValue} onChange={(e) => onConfig({ groupValue: e.target.value })} style={input()}>
                      <option value="">All</option>
                      {groupOptions.map((g) => (
                        <option key={g} value={g}>
                          {g}
                        </option>
                      ))}
                    </select>
                  </>
                ) : null}
                <label style={{ fontSize: 12, opacity: 0.7 }}>Aggregation</label>
                <select value={agg} onChange={(e) => onConfig({ agg: e.target.value })} style={input()}>
                  <option value="last">last</option>
                  <option value="avg">avg</option>
                  <option value="sum">sum</option>
                  <option value="min">min</option>
                  <option value="max">max</option>
                </select>
                <label style={{ fontSize: 12, opacity: 0.7 }}>Points</label>
                <input
                  type="number"
                  value={limit}
                  onChange={(e) => onConfig({ limit: Math.max(10, Number(e.target.value) || 120) })}
                  style={input()}
                />
              </>
            ) : null}
            <StockChart
              data={buildResultsSeries(resultsRows, fieldPath, groupBy, groupValue, agg, limit)}
              color="#34d399"
              showAxisLabels={prefs.showAxisLabels}
              labelDensity={prefs.labelDensity}
              tooltipDetail={prefs.tooltipDetail}
              xLabel="Time"
              yLabel={fieldPath}
            />
          </>
        ) : source === "report" ? (
          <>
            {expanded ? (
              <>
                <label style={{ fontSize: 12, opacity: 0.7 }}>Report</label>
                <select
                  value={widget.config.reportId || "crypto-index"}
                  onChange={(e) => onConfig({ reportId: e.target.value })}
                  style={input()}
                >
                  {reportOptions.map((it) => (
                    <option key={it.id} value={it.id}>
                      {it.name || it.id}
                    </option>
                  ))}
                </select>
              </>
            ) : null}
            <CustomReportChart reportId={widget.config.reportId || "crypto-index"} refreshMs={refreshMs} prefs={prefs} />
          </>
        ) : (
          <StockChart
            data={indexSeries}
            color="#4ea1ff"
            showAxisLabels={prefs.showAxisLabels}
            labelDensity={prefs.labelDensity}
            tooltipDetail={prefs.tooltipDetail}
            xLabel="Time"
            yLabel="Index"
          />
        )}
      </div>
    );
  }

  const size = (widget.config.size as WidgetSize) || "md";
  const span = size === "xl" ? "span 12" : size === "lg" ? "span 8" : size === "md" ? "span 4" : "span 3";
  const showPrefControls = expanded && (widget.type === "table" || widget.type === "chart");

  return (
    <div style={{ gridColumn: span, padding: 14, borderRadius: 12, border: "1px solid #1f2a37", background: "#0f141c" }}>
      <div style={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", marginBottom: 10 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flex: 1 }}>
          <input
            value={widget.title}
            onChange={(e) => onChange({ title: e.target.value })}
            style={{ ...input(), fontWeight: 800, width: "70%" }}
          />
          <span style={{ fontSize: 11, opacity: 0.6 }}>{widget.type.toUpperCase()}</span>
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          {layoutMode ? (
            <select
              value={size}
              onChange={(e) => onConfig({ size: e.target.value })}
              style={{ ...input(), padding: "4px 6px", fontSize: 12 }}
            >
              <option value="sm">sm</option>
              <option value="md">md</option>
              <option value="lg">lg</option>
              <option value="xl">xl</option>
            </select>
          ) : null}
          {layoutMode ? (
            <>
              <button onClick={onMoveUp} style={miniBtn()}>
                Up
              </button>
              <button onClick={onMoveDown} style={miniBtn()}>
                Down
              </button>
            </>
          ) : null}
          <button onClick={() => setExpanded((v) => !v)} style={miniBtn()}>
            {expanded ? "Compact" : "Expand"}
          </button>
          <button onClick={onDuplicate} style={miniBtn()}>
            Duplicate
          </button>
          <button onClick={onRemove} style={miniBtn()}>
            Remove
          </button>
        </div>
      </div>
      {showPrefControls ? (
        <div style={{ marginTop: 10, borderTop: "1px solid #1f2228", paddingTop: 8, display: "grid", gap: 6 }}>
          <div style={{ fontSize: 12, opacity: 0.75, display: "flex", justifyContent: "space-between" }}>
            <span>Chart prefs</span>
            <button
              onClick={() => {
                if (useCustomPrefs) {
                  onConfig({ usePrefs: false, prefs: undefined });
                } else {
                  onConfig({ usePrefs: true, prefs: { ...chartPrefs } });
                }
              }}
              style={miniBtn()}
            >
              {useCustomPrefs ? "Use global" : "Customize"}
            </button>
          </div>
          {useCustomPrefs ? (
            <>
              <label style={{ display: "grid", gap: 4 }}>
                <span style={{ fontSize: 12, opacity: 0.7 }}>Axis labels</span>
                <select
                  value={prefs.showAxisLabels ? "on" : "off"}
                  onChange={(e) => updatePrefs({ showAxisLabels: e.target.value === "on" })}
                  style={input()}
                >
                  <option value="on">On</option>
                  <option value="off">Off</option>
                </select>
              </label>
              <label style={{ display: "grid", gap: 4 }}>
                <span style={{ fontSize: 12, opacity: 0.7 }}>Label density</span>
                <select
                  value={prefs.labelDensity}
                  onChange={(e) => updatePrefs({ labelDensity: e.target.value as ChartPrefs["labelDensity"] })}
                  style={input()}
                >
                  <option value="full">Full</option>
                  <option value="sparse">Sparse</option>
                  <option value="none">None</option>
                </select>
              </label>
              <label style={{ display: "grid", gap: 4 }}>
                <span style={{ fontSize: 12, opacity: 0.7 }}>Max items</span>
                <input
                  type="number"
                  value={prefs.maxItems}
                  onChange={(e) => updatePrefs({ maxItems: Math.max(1, Number(e.target.value) || 1) })}
                  style={input()}
                />
              </label>
              <label style={{ display: "grid", gap: 4 }}>
                <span style={{ fontSize: 12, opacity: 0.7 }}>Tooltip detail</span>
                <select
                  value={prefs.tooltipDetail}
                  onChange={(e) => updatePrefs({ tooltipDetail: e.target.value as ChartPrefs["tooltipDetail"] })}
                  style={input()}
                >
                  <option value="minimal">Minimal</option>
                  <option value="full">Full</option>
                  <option value="full_ts">Full + timestamp</option>
                </select>
              </label>
              <label style={{ display: "grid", gap: 4 }}>
                <span style={{ fontSize: 12, opacity: 0.7 }}>Legend</span>
                <select
                  value={prefs.showLegend ? "on" : "off"}
                  onChange={(e) => updatePrefs({ showLegend: e.target.value === "on" })}
                  style={input()}
                >
                  <option value="off">Off</option>
                  <option value="on">On</option>
                </select>
              </label>
              <label style={{ display: "grid", gap: 4 }}>
                <span style={{ fontSize: 12, opacity: 0.7 }}>Click behavior</span>
                <select
                  value={prefs.clickBehavior}
                  onChange={(e) => updatePrefs({ clickBehavior: e.target.value as ChartPrefs["clickBehavior"] })}
                  style={input()}
                >
                  <option value="none">None</option>
                  <option value="focus">Focus</option>
                  <option value="filter">Filter page</option>
                </select>
              </label>
            </>
          ) : null}
        </div>
      ) : null}
      {body}
    </div>
  );
}

function Card({ title, value }: { title: string; value: any }) {
  return (
    <div style={{ padding: 12, borderRadius: 12, border: "1px solid #1f2a37", background: "#0b0f16" }}>
      <div style={{ fontSize: 12, opacity: 0.7 }}>{title}</div>
      <div style={{ fontSize: 20, fontWeight: 800 }}>{value ?? ""}</div>
    </div>
  );
}

function Table({
  rows,
  columns,
  onRowClick,
}: {
  rows: any[];
  columns: Array<{ key: string; label: string }>;
  onRowClick?: (row: any) => void;
}) {
  return (
    <div style={{ border: "1px solid #1f2228", borderRadius: 8, overflow: "auto" }}>
      <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c.key} style={{ textAlign: "left", padding: "8px 10px", borderBottom: "1px solid #1f2228" }}>
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={columns.length} style={{ padding: "10px", opacity: 0.6 }}>
                No data yet.
              </td>
            </tr>
          ) : (
            rows.map((r, idx) => (
              <tr
                key={r?.id || r?.symbol || idx}
                onClick={() => onRowClick?.(r)}
                style={onRowClick ? { cursor: "pointer" } : undefined}
              >
                {columns.map((c) => (
                  <td key={c.key} style={{ padding: "6px 10px", borderBottom: "1px solid #14161a" }}>
                    {formatCell(r, c.key)}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}

function formatCell(row: any, key: string): string {
  const value = row?.[key];
  if (value == null) return "";
  if (typeof value === "number") return Number(value).toFixed(4);
  if ((key === "timestamp" || key === "updated") && typeof value === "string") {
    const t = parseTime(value);
    if (t != null) return new Date(t).toLocaleString();
  }
  return String(value);
}

function CustomReportChart({
  reportId,
  refreshMs,
  prefs,
}: {
  reportId: string;
  refreshMs: number;
  prefs?: ChartPrefs;
}) {
  const [points, setPoints] = useState<{ t: number; v: number }[]>([]);
  const abortRef = useRef<AbortController | null>(null);
  const lastRef = useRef<number>(0);

  useEffect(() => {
    let mounted = true;
    let t: number | undefined;
    const refresh = async () => {
      if (abortRef.current) abortRef.current.abort();
      const ctrl = new AbortController();
      abortRef.current = ctrl;
      const data = await fetchJson(`/api/reports/${encodeURIComponent(reportId)}`, 8000, ctrl.signal);
      if (mounted && data) {
        const s = Array.isArray(data.series) ? data.series[0] : null;
        const pts = Array.isArray(s?.points) ? s.points : [];
        const normalized = pts
          .map((p: any) => {
            const tt = parseTime(p?.t);
            const vv = typeof p?.y === "number" ? p.y : typeof p?.v === "number" ? p.v : null;
            if (tt == null || vv == null) return null;
            return { t: tt, v: vv };
          })
          .filter(Boolean) as { t: number; v: number }[];
        normalized.sort((a, b) => a.t - b.t);
        const next = normalized.slice(-600);
        const latest = next.length ? next[next.length - 1].t : 0;
        if (latest !== lastRef.current) {
          lastRef.current = latest;
          setPoints(next);
        }
      }
      t = window.setTimeout(refresh, Math.max(1500, refreshMs));
    };
    refresh();
    return () => {
      mounted = false;
      if (abortRef.current) abortRef.current.abort();
      if (t) window.clearTimeout(t);
    };
  }, [reportId, refreshMs]);

  return (
    <StockChart
      data={points}
      color="#4ea1ff"
      showAxisLabels={prefs?.showAxisLabels}
      labelDensity={prefs?.labelDensity}
      tooltipDetail={prefs?.tooltipDetail}
      xLabel="Time"
      yLabel="Value"
    />
  );
}

function formatResultSummary(row: any): string {
  const data = row?.data;
  if (data == null) return "";
  let raw = "";
  if (typeof data === "string") raw = data;
  else {
    try {
      raw = JSON.stringify(data);
    } catch {
      raw = String(data);
    }
  }
  if (raw.length > 140) return `${raw.slice(0, 137)}...`;
  return raw;
}

function getFieldValue(row: any, path: string): any {
  if (!path) return undefined;
  const parts = path.split(".").map((p) => p.trim()).filter(Boolean);
  let cur: any = row;
  for (const p of parts) {
    if (cur == null) return undefined;
    cur = cur[p];
  }
  return cur;
}

function buildGroupOptions(rows: any[], path: string): string[] {
  if (!path) return [];
  const set = new Set<string>();
  for (const r of rows) {
    const v = getFieldValue(r, path);
    if (v == null) continue;
    set.add(String(v));
  }
  return Array.from(set).slice(0, 50);
}

function buildResultsSeries(
  rows: any[],
  fieldPath: string,
  groupBy: string,
  groupValue: string,
  agg: string,
  limit: number
): { t: number; v: number }[] {
  if (!rows || rows.length === 0) return [];
  const filtered = rows.filter((r) => {
    if (!groupBy) return true;
    const v = getFieldValue(r, groupBy);
    if (groupValue) return String(v) === String(groupValue);
    return true;
  });
  const buckets = new Map<number, number[]>();
  for (const r of filtered) {
    const t = parseTime(r?.timestamp || r?.ts || "");
    const raw = getFieldValue(r, fieldPath);
    const v = typeof raw === "number" ? raw : Number(raw);
    if (t == null || !Number.isFinite(v)) continue;
    const arr = buckets.get(t) || [];
    arr.push(v);
    buckets.set(t, arr);
  }
  const points: Array<{ t: number; v: number }> = Array.from(buckets.entries()).map(([t, vals]) => {
    let v = vals[vals.length - 1];
    if (agg === "avg") v = vals.reduce((a, b) => a + b, 0) / vals.length;
    if (agg === "sum") v = vals.reduce((a, b) => a + b, 0);
    if (agg === "min") v = Math.min(...vals);
    if (agg === "max") v = Math.max(...vals);
    return { t, v };
  });
  points.sort((a, b) => a.t - b.t);
  return points.slice(-limit);
}

function buildBestResultsSeries(rows: any[], limit: number): { t: number; v: number }[] {
  if (!rows || rows.length === 0) return [];
  const candidates = [
    "data.c",
    "data.price_usd",
    "data.price",
    "data.last",
    "data.close",
    "price",
    "last",
    "value",
    "v",
  ];
  let best: { t: number; v: number }[] = [];
  for (const field of candidates) {
    const series = buildResultsSeries(rows, field, "", "", "last", limit);
    if (series.length > best.length) best = series;
    if (series.length >= 2) return series;
  }
  return best;
}

function appendSymbolSeries(
  prev: Record<string, { t: number; v: number }[]>,
  rows: any[],
  limit: number
): Record<string, { t: number; v: number }[]> {
  if (!Array.isArray(rows) || rows.length === 0) return prev;
  const now = Date.now();
  const next: Record<string, { t: number; v: number }[]> = { ...prev };
  for (const row of rows) {
    const sym = String(row?.symbol || "").trim();
    const raw = row?.price ?? row?.last ?? row?.close ?? row?.c;
    const value = typeof raw === "number" ? raw : Number(raw);
    if (!sym || !Number.isFinite(value)) continue;
    const current = next[sym] ? next[sym].slice() : [];
    if (current.length > 0 && now - current[current.length - 1].t < 900) {
      current[current.length - 1] = { t: now, v: value };
    } else {
      current.push({ t: now, v: value });
    }
    next[sym] = current.slice(-limit);
  }
  return next;
}

function buildSymbolProfiles(
  rows: any[],
  seriesMap: Record<string, { t: number; v: number }[]>
): Array<{ id: string; name?: string }> {
  const set = new Set<string>();
  for (const row of rows || []) {
    const sym = String(row?.symbol || "").trim();
    if (sym) set.add(sym);
  }
  for (const [sym, series] of Object.entries(seriesMap || {})) {
    if (!sym) continue;
    if (!Array.isArray(series) || series.length === 0) continue;
    set.add(sym);
  }
  return Array.from(set)
    .sort((a, b) => a.localeCompare(b))
    .map((sym) => ({ id: sym, name: sym }));
}

function input(): React.CSSProperties {
  return { padding: "6px 8px", borderRadius: 6, border: "1px solid #1f2228", background: "#0b0c10", color: "#f3f4f6" };
}

function miniBtn(): React.CSSProperties {
  return { padding: "4px 6px", borderRadius: 6, border: "1px solid #1f2228", background: "#111827", color: "#e5e7eb", cursor: "pointer", fontSize: 12 };
}

function AddWidgetMenu({ onAdd }: { onAdd: (type: CustomWidgetType) => void }) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      const target = e.target as Node;
      if (menuRef.current && !menuRef.current.contains(target)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  return (
    <div ref={menuRef} style={{ position: "relative" }}>
      <button
        onClick={() => setOpen((v) => !v)}
        style={{
          padding: "6px 10px",
          borderRadius: 8,
          border: "1px solid #1f2228",
          background: "#0f1115",
          color: "#9ca3af",
          cursor: "pointer",
        }}
      >
        Add Widget
      </button>
      {open ? (
        <div
          style={{
            position: "absolute",
            right: 0,
            top: "110%",
            background: "#0f1115",
            border: "1px solid #1f2228",
            borderRadius: 8,
            minWidth: 160,
            padding: 6,
            zIndex: 20,
          }}
        >
          {(["kpi", "note", "table", "chart"] as CustomWidgetType[]).map((t) => (
            <button
              key={t}
              onClick={() => {
                onAdd(t);
                setOpen(false);
              }}
              style={{
                display: "block",
                width: "100%",
                textAlign: "left",
                padding: "6px 8px",
                borderRadius: 6,
                border: "1px solid transparent",
                background: "#0f1115",
                color: "#e5e7eb",
                cursor: "pointer",
              }}
            >
              Add {t.toUpperCase()}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
