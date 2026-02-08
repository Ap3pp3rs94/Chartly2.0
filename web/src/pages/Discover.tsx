import React, { useMemo, useState } from "react";
import { getVirtualProfiles, setVirtualProfiles, VirtualProfile, getVars } from "@/lib/storage";

type Template = {
  id: string;
  name: string;
  description: string;
  url: string;
  kind: "crypto" | "stocks" | "generic";
};

const TEMPLATES: Template[] = [
  {
    id: "crypto-live",
    name: "Crypto Prices (Live)",
    description: "Live crypto prices without API keys. Uses a public market endpoint.",
    url: "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum,solana&vs_currencies=usd&include_24hr_change=true",
    kind: "crypto"
  },
  {
    id: "stocks-live",
    name: "Stocks Quotes (Live)",
    description: "Free stock quotes without API keys (limited).",
    url: "https://stooq.com/q/l/?s=aapl.us,msft.us,tsla.us&f=sd2t2ohlcv&h&e=json",
    kind: "stocks"
  }
];

function slugify(s: string) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
}

function shortHash(s: string) {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = (h * 16777619) >>> 0;
  }
  return h.toString(16).slice(0, 8);
}

function createVirtualProfile(name: string, url: string, origin: string): VirtualProfile {
  const base = slugify(name || "source");
  const id = `src-${base}-${shortHash(url)}`;
  return {
    id,
    name: name || "Source",
    version: "0.1.0",
    description: "Imported source (virtual).",
    source: { type: "http_rest", url, auth: "none" },
    mapping: {},
    _virtual: true,
    _origin: origin
  };
}

function getRegistryKey(): string {
  const vars = getVars();
  return vars["REGISTRY_API_KEY"] || vars["X_API_KEY"] || vars["API_KEY"] || "";
}

function buildSourceProfileYaml(vp: VirtualProfile) {
  const safeName = vp.name || vp.id;
  return `id: ${vp.id}
name: ${safeName}
version: "1.0"
description: "Imported source (virtual)."

source:
  type: http_rest
  url: ${vp.source?.url || ""}
  auth: none
  format: json
`;
}

export default function Discover() {
  const [customName, setCustomName] = useState("");
  const [customUrl, setCustomUrl] = useState("");
  const [message, setMessage] = useState("");
  const vars = useMemo(() => getVars(), []);

  const addVirtual = (vp: VirtualProfile) => {
    const list = getVirtualProfiles();
    if (list.some((p) => p.id === vp.id)) {
      setMessage("Source already added.");
      return;
    }
    list.push(vp);
    setVirtualProfiles(list);
    setMessage(`Added ${vp.name}.`);
  };

  const registerProfile = async (vp: VirtualProfile) => {
    const key = getRegistryKey();
    if (!key) {
      setMessage("Missing REGISTRY_API_KEY (set it in Settings).");
      return;
    }
    try {
      const res = await fetch("/api/profiles", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-API-Key": key },
        body: JSON.stringify({
          id: vp.id,
          name: vp.name || vp.id,
          version: "1.0",
          content: buildSourceProfileYaml(vp),
        }),
      });
      if (res.ok) {
        setMessage(`Registered ${vp.name || vp.id}.`);
        return;
      }
      const err = await res.json().catch(() => ({}));
      setMessage(`Register failed: ${err?.error || res.status}`);
    } catch {
      setMessage("Register failed.");
    }
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <h1 style={{ margin: 0, fontSize: 20 }}>Add Sources</h1>

      <div style={{ padding: 12, borderRadius: 6, border: "1px solid #1f2228", background: "#0f1115" }}>
        <div style={{ fontWeight: 700, marginBottom: 6 }}>Plug-and-play sources</div>
        <div style={{ fontSize: 12, opacity: 0.7 }}>
          Add live sources without vendor lock-in. These sources are stored locally as virtual profiles until you register them.
        </div>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(2, minmax(0,1fr))", gap: 10 }}>
        {TEMPLATES.map((t) => (
          <div key={t.id} style={{ padding: 12, borderRadius: 6, border: "1px solid #1f2228", background: "#0f1115" }}>
            <div style={{ fontWeight: 700 }}>{t.name}</div>
            <div style={{ fontSize: 12, opacity: 0.7, marginTop: 6 }}>{t.description}</div>
            <div style={{ fontSize: 11, opacity: 0.6, marginTop: 6, wordBreak: "break-all" }}>{t.url}</div>
            <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
              <button
                style={{ padding: "6px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}
                onClick={() => addVirtual(createVirtualProfile(t.name, t.url, t.kind))}
              >
                Add to Workspace
              </button>
              <button
                style={{ padding: "6px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#111827", color: "#e5e7eb" }}
                onClick={() => registerProfile(createVirtualProfile(t.name, t.url, t.kind))}
              >
                Register
              </button>
              <button
                style={{ padding: "6px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#101216", color: "#9aa0aa" }}
                onClick={() => window.location.assign("/profiles")}
              >
                Manage
              </button>
            </div>
          </div>
        ))}
      </div>

      <div style={{ padding: 12, borderRadius: 6, border: "1px solid #1f2228", background: "#0f1115" }}>
        <div style={{ fontWeight: 700 }}>Custom Source</div>
        <div style={{ fontSize: 12, opacity: 0.7, marginTop: 6 }}>Paste a URL to a JSON or RSS feed. No API keys required.</div>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 2fr", gap: 8, marginTop: 10 }}>
          <input
            style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}
            placeholder="Name"
            value={customName}
            onChange={(e) => setCustomName(e.target.value)}
          />
          <input
            style={{ padding: "8px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#f3f4f6" }}
            placeholder="https://example.com/data.json"
            value={customUrl}
            onChange={(e) => setCustomUrl(e.target.value)}
          />
        </div>
        <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
          <button
            style={{ padding: "6px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#14161a", color: "#f3f4f6" }}
            onClick={() => {
              if (!customUrl.trim()) {
                setMessage("Add a URL first.");
                return;
              }
              addVirtual(createVirtualProfile(customName || "Custom Source", customUrl.trim(), "generic"));
              setCustomUrl("");
              setCustomName("");
            }}
          >
            Add to Workspace
          </button>
          <button
            style={{ padding: "6px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#111827", color: "#e5e7eb" }}
            onClick={() => {
              if (!customUrl.trim()) {
                setMessage("Add a URL first.");
                return;
              }
              registerProfile(createVirtualProfile(customName || "Custom Source", customUrl.trim(), "generic"));
            }}
          >
            Register
          </button>
          <button
            style={{ padding: "6px 10px", borderRadius: 4, border: "1px solid #1f2228", background: "#0f1115", color: "#9aa0aa" }}
            onClick={() => window.location.assign("/settings")}
          >
            Settings
          </button>
        </div>
      </div>

      <div style={{ padding: 10, fontSize: 12, opacity: 0.7 }}>
        {message ? message : `Variables loaded: ${Object.keys(vars).length}`}
      </div>
    </div>
  );
}
