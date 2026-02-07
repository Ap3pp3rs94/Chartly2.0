import { getVirtualProfiles } from "@/lib/storage";

export type Profile = {
  id: string;
  name?: string;
  version?: string;
  description?: string;
  _virtual?: boolean;
};

export async function fetchRealProfiles(): Promise<Profile[]> {
  try {
    const res = await fetch("/api/profiles");
    if (!res.ok) return [];
    const data = await res.json();
    const list = Array.isArray(data) ? data : data?.profiles ?? [];
    return list
      .map((p: any) => ({ id: p.id, name: p.name, version: p.version, description: p.description }))
      .filter((p: any) => p.id);
  } catch {
    return [];
  }
}

export async function getAllProfiles(): Promise<Profile[]> {
  const real = await fetchRealProfiles();
  const virt = getVirtualProfiles().map((p) => ({ id: p.id, name: p.name, version: p.version, description: p.description, _virtual: true }));
  const map = new Map<string, Profile>();
  for (const p of real) map.set(p.id, { ...p, _virtual: false });
  for (const p of virt) if (!map.has(p.id)) map.set(p.id, p);
  return Array.from(map.values()).sort((a, b) => a.id.localeCompare(b.id));
}
