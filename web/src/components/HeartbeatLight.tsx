import React from "react";

export type HeartbeatState = "live" | "connecting" | "stale" | "dead";

type Props = { state: HeartbeatState; title?: string };

export default function HeartbeatLight({ state, title }: Props) {
  const cls = state === "live" ? "hbDot good" : state === "dead" ? "hbDot bad" : state === "stale" ? "hbDot warn" : "hbDot warn";
  const label = title || state;
  return <span className={cls} aria-label={`live status ${label}`} title={label} />;
}
