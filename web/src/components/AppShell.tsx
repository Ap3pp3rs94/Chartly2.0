import React, { PropsWithChildren } from "react";
import HeartbeatLight, { HeartbeatState } from "@/components/HeartbeatLight";

type Props = PropsWithChildren<{
  heartbeat: HeartbeatState;
  title?: string;
  onSettings?: () => void;
}>;

export default function AppShell({ heartbeat, title, onSettings, children }: Props) {
  return (
    <div className="app-shell">
      <header className="app-topbar">
        <div className="brand">{title || "Chartly"}</div>
        <div className="actions">
          <HeartbeatLight state={heartbeat} title={heartbeat} />
          {onSettings ? (
            <button className="btn" onClick={onSettings}>
              Settings
            </button>
          ) : null}
        </div>
      </header>
      <main className="app-main">{children}</main>
    </div>
  );
}
