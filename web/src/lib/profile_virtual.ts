import { getVirtualProfiles } from "@/lib/storage";

export type Profile = {
  id: string;
  name?: string;
  version?: string;
  description?: string;
  _virtual?: boolean;
  _origin?: "registry" | "gateway" | "derived_symbol" | "virtual";
};

export async function fetchRealProfiles(): Promise<Profile[]> {
  try {
    const res = await fetch("/api/profiles");
    if (!res.ok) return [];
    const data = await res.json();
    const list = Array.isArray(data) ? data : data?.profiles ?? [];
    return list
      .map((p: any) => ({
        id: String(p?.id || "").trim(),
        name: String(p?.name || "").trim() || undefined,
        version: String(p?.version || "").trim() || undefined,
        description: String(p?.description || "").trim() || undefined,
        _origin: "registry" as const,
      }))
      .filter((p: any) => p.id);
  } catch {
    return [];
  }
}

export async function fetchGatewayProfiles(): Promise<Profile[]> {
  try {
    const res = await fetch("/v1/profiles", {
      headers: {
        accept: "application/json",
        "x-request-id": `profiles-page-${Date.now()}`,
        traceparent: "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
      },
    });
    if (!res.ok) return [];
    const data = await res.json();
    const list = Array.isArray(data?.profiles) ? data.profiles : Array.isArray(data) ? data : [];
    return list
      .map((p: any) => {
        const id = String(p?.id || p?.profile_id || "").trim();
        if (!id) return null;
        return {
          id,
          name: id,
          description: "Gateway profile",
          _origin: "gateway" as const,
        };
      })
      .filter(Boolean) as Profile[];
  } catch {
    return [];
  }
}

export async function fetchDerivedSymbolProfiles(): Promise<Profile[]> {
  try {
    const res = await fetch("/api/crypto/top?limit=100");
    if (!res.ok) return [];
    const data = await res.json();
    if (!Array.isArray(data)) return [];
    const set = new Set<string>();
    for (const row of data) {
      const symbol = String(row?.symbol || "").trim();
      if (symbol) set.add(symbol);
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((symbol) => ({
        id: symbol,
        name: symbol,
        description: "Derived from live crypto feed",
        _virtual: true,
        _origin: "derived_symbol" as const,
      }));
  } catch {
    return [];
  }
}

export async function getAllProfiles(): Promise<Profile[]> {
  const [registry, gateway, derived] = await Promise.all([
    fetchRealProfiles(),
    fetchGatewayProfiles(),
    fetchDerivedSymbolProfiles(),
  ]);
  const virt = getVirtualProfiles().map((p) => ({
    id: p.id,
    name: p.name,
    version: p.version,
    description: p.description,
    _virtual: true,
    _origin: "virtual" as const,
  }));
  const map = new Map<string, Profile>();
  for (const p of gateway) map.set(p.id, { ...p, _virtual: false });
  for (const p of registry) map.set(p.id, { ...p, _virtual: false });
  for (const p of derived) if (!map.has(p.id)) map.set(p.id, p);
  for (const p of virt) if (!map.has(p.id)) map.set(p.id, p);
  return Array.from(map.values()).sort((a, b) => a.id.localeCompare(b.id));
}
