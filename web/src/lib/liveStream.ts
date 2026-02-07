export type LiveState = "connecting" | "live" | "stale" | "dead";

export type LiveEvent =
  | { type: "hello"; data: any }
  | { type: "heartbeat"; data: any }
  | { type: "results"; data: { ts: string; items: any[] } }
  | { type: "error"; data: any };

type LiveOpts = {
  profiles?: string[];
  intervalMs?: number;
  limit?: number;
  onEvent: (ev: LiveEvent) => void;
  onState?: (state: LiveState) => void;
};

export function connectLiveStream(opts: LiveOpts) {
  let es: EventSource | null = null;
  const backoff = [250, 500, 1000, 2000, 4000];
  let backoffIdx = 0;
  let lastEventAt = Date.now();
  let buffer: any[] = [];
  let flushTimer: number | null = null;
  let closed = false;

  const start = () => {
    if (closed) return;
    opts.onState?.("connecting");

    const params = new URLSearchParams();
    if (opts.profiles && opts.profiles.length) params.set("profiles", opts.profiles.join(","));
    if (opts.intervalMs) params.set("interval_ms", String(opts.intervalMs));
    if (opts.limit) params.set("limit", String(opts.limit));

    es = new EventSource(`/api/live/stream?${params.toString()}`);

    es.onopen = () => {
      backoffIdx = 0;
      opts.onState?.("live");
    };

    es.addEventListener("hello", (e: MessageEvent) => {
      lastEventAt = Date.now();
      opts.onEvent({ type: "hello", data: safeParse(e.data) });
    });

    es.addEventListener("heartbeat", (e: MessageEvent) => {
      lastEventAt = Date.now();
      opts.onEvent({ type: "heartbeat", data: safeParse(e.data) });
    });

    es.addEventListener("results", (e: MessageEvent) => {
      lastEventAt = Date.now();
      const data = safeParse(e.data) || { items: [] };
      if (data && Array.isArray(data.items)) {
        buffer.push(...data.items);
      }
    });

    es.addEventListener("error", (e: MessageEvent) => {
      lastEventAt = Date.now();
      opts.onEvent({ type: "error", data: safeParse(e.data) });
    });

    es.onerror = () => {
      cleanup();
      const wait = backoff[Math.min(backoffIdx, backoff.length - 1)];
      backoffIdx++;
      opts.onState?.("stale");
      setTimeout(start, wait);
    };

    if (!flushTimer) {
      flushTimer = window.setInterval(() => {
        const age = Date.now() - lastEventAt;
        if (age > 10000) opts.onState?.("stale");
        if (age > 30000) opts.onState?.("dead");
        flush();
      }, 250);
    }
  };

  const flush = () => {
    if (!buffer.length) return;
    const items = buffer;
    buffer = [];
    opts.onEvent({ type: "results", data: { ts: new Date().toISOString(), items } });
  };

  const cleanup = () => {
    if (es) {
      es.close();
      es = null;
    }
  };

  start();

  return () => {
    closed = true;
    cleanup();
    if (flushTimer) window.clearInterval(flushTimer);
  };
}

function safeParse(s: string) {
  try {
    return JSON.parse(s);
  } catch {
    return null;
  }
}

export function toTitleLabel(path: string) {
  const p = path.replace(/\[0\]/g, "");
  const last = p.split(".").pop() || p;
  const cleaned = last.replace(/[_-]/g, " ");
  return cleaned
    .split(" ")
    .map((w) => (w.toUpperCase() === w ? w : w.charAt(0).toUpperCase() + w.slice(1)))
    .join(" ");
}
