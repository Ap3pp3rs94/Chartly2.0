import React, { useEffect, useMemo, useRef, useState } from "react";
function ensureStyles() {
  if (document.getElementById("status-pulse-style")) return;
  const style = document.createElement("style");
  style.id = "status-pulse-style";
  style.textContent = `
  @keyframes pulseGreen { 0%{box-shadow:0 0 0 rgba(50,213,131,0.2);} 70%{box-shadow:0 0 10px rgba(50,213,131,0.8);} 100%{box-shadow:0 0 0 rgba(50,213,131,0.2);} }
  @keyframes pulseRed { 0%{box-shadow:0 0 0 rgba(255,92,122,0.2);} 70%{box-shadow:0 0 10px rgba(255,92,122,0.8);} 100%{box-shadow:0 0 0 rgba(255,92,122,0.2);} }
  `;
  document.head.appendChild(style);
}

type PulseState = "ok" | "down";

export default function StatusPulse() {
  const [state, setState] = useState<PulseState>("down");
  const lastOk = useRef<number | null>(null);

  useEffect(() => {
    ensureStyles();
    let alive = true;
    const interval = 2000;

    const poll = async () => {
      if (!alive) return;
      try {
        const res = await fetch("/health");
        if (res.ok) {
          lastOk.current = Date.now();
          setState("ok");
        } else {
          setState("down");
        }
      } catch {
        setState("down");
      }
    };

    poll();
    const t = setInterval(poll, interval);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);

  const title = useMemo(() => {
    if (state === "ok" && lastOk.current) {
      const delta = Date.now() - lastOk.current;
      const s = Math.floor(delta / 1000);
      return `Healthy (last ok: ${s}s ago)`;
    }
    return "Unhealthy";
  }, [state]);

  const color = state === "ok" ? "#32d583" : "#ff5c7a";
  const anim = state === "ok" ? "pulseGreen 2s infinite" : "pulseRed 2s infinite";

  return (
    <div title={title} aria-label={`status ${state}`} style={{ width: 10, height: 10, borderRadius: 999, background: color, animation: anim }} />
  );
}


