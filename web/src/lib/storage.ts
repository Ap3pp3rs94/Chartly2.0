export type Settings = {
  refreshMs: number;
  summaryCadenceMin: number;
  defaultRange: string;
};

export type VarsMap = Record<string, string>;

export type VirtualProfile = {
  id: string;
  name: string;
  version: string;
  description?: string;
  source: { type: string; url: string; auth: string };
  mapping: Record<string, string>;
  _virtual?: boolean;
  _origin?: string;
};

const KEY_SETTINGS = "chartly.settings.v1";
const KEY_VARS = "chartly.vars.v1";
const KEY_VIRTUAL = "chartly.profiles.virtual.v1";

const defaultSettings: Settings = {
  refreshMs: 25000,
  summaryCadenceMin: 10,
  defaultRange: "last_hour"
};

export function getSettings(): Settings {
  try {
    const raw = localStorage.getItem(KEY_SETTINGS);
    if (!raw) return { ...defaultSettings };
    const v = JSON.parse(raw);
    return {
      refreshMs: Number(v.refreshMs) || defaultSettings.refreshMs,
      summaryCadenceMin: Number(v.summaryCadenceMin) || defaultSettings.summaryCadenceMin,
      defaultRange: String(v.defaultRange || defaultSettings.defaultRange)
    };
  } catch {
    return { ...defaultSettings };
  }
}

export function setSettings(s: Settings): void {
  localStorage.setItem(KEY_SETTINGS, JSON.stringify(s));
}

export function getVars(): VarsMap {
  try {
    const raw = localStorage.getItem(KEY_VARS);
    if (!raw) return {};
    const v = JSON.parse(raw);
    return v && typeof v === "object" ? v : {};
  } catch {
    return {};
  }
}

export function setVars(vars: VarsMap): void {
  localStorage.setItem(KEY_VARS, JSON.stringify(vars));
}

export function getVirtualProfiles(): VirtualProfile[] {
  try {
    const raw = localStorage.getItem(KEY_VIRTUAL);
    if (!raw) return [];
    const v = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return v;
  } catch {
    return [];
  }
}

export function setVirtualProfiles(profiles: VirtualProfile[]): void {
  localStorage.setItem(KEY_VIRTUAL, JSON.stringify(profiles));
}
