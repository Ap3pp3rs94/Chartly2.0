export type FieldInfo = { path: string; label: string; type: "string" | "number" | "boolean" | "array"; sample?: any };

export type ReportKind = "candlestick" | "timeseries_line" | "bar_topN" | "scatter" | "table";

function hasKeyword(s: string, kws: string[]): boolean {
  const t = s.toLowerCase();
  return kws.some((k) => t.includes(k));
}

export function suggestReport(fields: FieldInfo[]): {
  kind: ReportKind;
  timeField?: FieldInfo;
  metricField?: FieldInfo;
  metricField2?: FieldInfo;
  ohlc?: { open: FieldInfo; high: FieldInfo; low: FieldInfo; close: FieldInfo };
} {
  const timeCandidates = fields.filter((f) => {
    if (f.type === "number" && hasKeyword(f.path + " " + f.label, ["time", "timestamp", "date", "year", "month", "day", "ts"])) return true;
    if (f.type === "string" && hasKeyword(f.path + " " + f.label, ["time", "timestamp", "date"])) return true;
    return false;
  });

  const numeric = fields.filter((f) => f.type === "number");

  const o = fields.find((f) => /open/i.test(f.path + f.label) && f.type === "number");
  const h = fields.find((f) => /high/i.test(f.path + f.label) && f.type === "number");
  const l = fields.find((f) => /low/i.test(f.path + f.label) && f.type === "number");
  const c = fields.find((f) => /close/i.test(f.path + f.label) && f.type === "number");

  if (o && h && l && c && timeCandidates.length > 0) {
    return { kind: "candlestick", timeField: timeCandidates[0], metricField: c, ohlc: { open: o, high: h, low: l, close: c } };
  }

  if (timeCandidates.length > 0 && numeric.length > 0) {
    return { kind: "timeseries_line", timeField: timeCandidates[0], metricField: numeric[0] };
  }

  if (numeric.length >= 2) {
    return { kind: "scatter", metricField: numeric[0], metricField2: numeric[1] };
  }

  const hasCategory = fields.some((f) => f.type === "string");
  if (numeric.length > 0 && hasCategory) {
    return { kind: "bar_topN", metricField: numeric[0] };
  }

  return { kind: "table" };
}
