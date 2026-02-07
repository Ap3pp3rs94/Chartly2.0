import { useCallback, useEffect, useRef, useState } from "react";

export function usePolling<T>(
  fn: () => Promise<T>,
  intervalMs: number,
  opts?: { enabled?: boolean; immediate?: boolean }
): { data: T | null; error: string | null; running: boolean; refresh: () => void } {
  const enabled = opts?.enabled !== false;
  const immediate = opts?.immediate !== false;

  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [running, setRunning] = useState<boolean>(false);

  const fnRef = useRef(fn);
  const timerRef = useRef<number | null>(null);
  const aliveRef = useRef<boolean>(true);

  fnRef.current = fn;

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const run = useCallback(async () => {
    if (!enabled) return;
    setRunning(true);
    try {
      const res = await fnRef.current();
      if (!aliveRef.current) return;
      setData(res);
      setError(null);
    } catch (e: any) {
      if (!aliveRef.current) return;
      setError(String(e?.message ?? e));
    } finally {
      if (aliveRef.current) setRunning(false);
    }
  }, [enabled]);

  useEffect(() => {
    aliveRef.current = true;
    clearTimer();

    if (!enabled) {
      setRunning(false);
      return () => {
        aliveRef.current = false;
        clearTimer();
      };
    }

    const ms = Math.max(50, Math.floor(intervalMs || 0));
    if (immediate) run();

    timerRef.current = window.setInterval(run, ms);

    return () => {
      aliveRef.current = false;
      clearTimer();
    };
  }, [enabled, immediate, intervalMs, run, clearTimer]);

  return { data, error, running, refresh: run };
}
