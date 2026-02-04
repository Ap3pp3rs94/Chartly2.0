import { useEffect, useMemo, useRef, useState } from "react";

export type WSState = "idle" | "connecting" | "open" | "closed" | "error";

export type UseWebSocketOptions = {
  protocols?: string | string[];
  autoConnect?: boolean;
};

const backoffSeq = [250, 500, 1000, 2000, 5000];

export function useWebSocket(
  url: string | null,
  opts?: UseWebSocketOptions
): {
  state: WSState;
  lastMessage: string | null;
  error: string | null;
  send: (data: string) => boolean;
  close: () => void;
} {
  const protocols = opts?.protocols;
  const autoConnect = opts?.autoConnect !== false;

  const [state, setState] = useState<WSState>(() => (url ? "connecting" : "idle"));
  const [lastMessage, setLastMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const timerRef = useRef<number | null>(null);
  const attemptRef = useRef<number>(0);
  const urlRef = useRef<string | null>(url);

  const canConnect = useMemo(() => Boolean(url && autoConnect), [url, autoConnect]);

  function clearTimer() {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }

  function cleanupSocket() {
    const ws = wsRef.current;
    wsRef.current = null;
    if (ws) {
      try {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onerror = null;
        ws.onclose = null;
        ws.close();
      } catch {
        // ignore
      }
    }
  }

  function scheduleReconnect() {
    clearTimer();
    const attempt = attemptRef.current;
    const ms = backoffSeq[Math.min(attempt, backoffSeq.length - 1)];
    attemptRef.current = attempt + 1;
    timerRef.current = window.setTimeout(() => {
      connect();
    }, ms);
  }

  function connect() {
    if (!urlRef.current || !autoConnect) return;

    cleanupSocket();
    clearTimer();

    setState("connecting");
    setError(null);

    try {
      const ws = new WebSocket(urlRef.current, protocols as any);
      wsRef.current = ws;

      ws.onopen = () => {
        attemptRef.current = 0;
        setState("open");
        setError(null);
      };

      ws.onmessage = (ev) => {
        setLastMessage(typeof ev.data === "string" ? ev.data : String(ev.data));
      };

      ws.onerror = () => {
        setState("error");
        setError("websocket error");
        // let close handler schedule reconnect
      };

      ws.onclose = () => {
        // If url is still active and autoConnect, reconnect deterministically.
        if (urlRef.current && autoConnect) {
          setState("closed");
          scheduleReconnect();
        } else {
          setState("closed");
        }
      };
    } catch (e: any) {
      setState("error");
      setError(String(e?.message ?? e));
      if (urlRef.current && autoConnect) scheduleReconnect();
    }
  }

  useEffect(() => {
    urlRef.current = url;

    // Reset attempt counter when URL changes.
    attemptRef.current = 0;
    clearTimer();
    cleanupSocket();

    if (!url) {
      setState("idle");
      setError(null);
      setLastMessage(null);
      return;
    }

    if (!autoConnect) {
      setState("closed");
      return;
    }

    connect();

    return () => {
      clearTimer();
      cleanupSocket();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, autoConnect, JSON.stringify(protocols ?? null)]);

  function send(data: string): boolean {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return false;
    try {
      ws.send(data);
      return true;
    } catch {
      return false;
    }
  }

  function close() {
    clearTimer();
    urlRef.current = null;
    cleanupSocket();
    setState("closed");
  }

  // If url exists but autoConnect becomes true later, connect.
  useEffect(() => {
    if (canConnect && state !== "open" && state !== "connecting") {
      connect();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canConnect]);

  return { state, lastMessage, error, send, close };
}
