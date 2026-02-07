export type LiveItem = { profileId: string; points: Array<{ t: number; v: number }>; lastTs?: string };

export type LiveState = "idle" | "live" | "stale";

export type LiveOptions = {
  profiles: string[];
  intervalMs: number;
  limit: number;
  fetcher: (profileId: string, limit: number) => Promise<any[]>;
  onUpdate: (items: LiveItem[]) => void;
};

export function createLivePoller(opts: LiveOptions) {
  let alive = true;
  let timer: any = null;
  let last = 0;
  const cache = new Map<string, LiveItem>();

  async function tick() {
    if (!alive) return;
    const now = Date.now();
    if (now - last < opts.intervalMs) return;
    last = now;

    const items: LiveItem[] = [];
    for (const pid of opts.profiles) {
      try {
        const rows = await opts.fetcher(pid, opts.limit);
        const pts = rows as any[];
        const item: LiveItem = cache.get(pid) || { profileId: pid, points: [] };
        item.points = pts as any;
        cache.set(pid, item);
        items.push(item);
      } catch {
        // ignore per-profile errors
      }
    }
    if (items.length) opts.onUpdate(items);
  }

  function start() {
    timer = setInterval(tick, opts.intervalMs);
    tick();
  }

  function stop() {
    alive = false;
    if (timer) clearInterval(timer);
  }

  start();
  return { stop };
}
