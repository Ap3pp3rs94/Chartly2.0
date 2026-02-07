import React, { useEffect, useMemo, useState } from "react";

type Profile = { id: string; name?: string };

type Props = {
  selected: string[];
  onChange: (ids: string[]) => void;
  title?: string;
};

export default function ProfileMultiSelect({ selected, onChange, title }: Props) {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [query, setQuery] = useState("");

  useEffect(() => {
    fetch("/api/profiles")
      .then((r) => (r.ok ? r.json() : []))
      .then((data) => {
        const list = Array.isArray(data) ? data : data?.profiles ?? [];
        setProfiles(list.map((p: any) => ({ id: p.id, name: p.name || p.id })));
      })
      .catch(() => setProfiles([]));
  }, []);

  const filtered = useMemo(() => {
    const q = query.toLowerCase();
    return profiles.filter((p) => p.name?.toLowerCase().includes(q) || p.id.toLowerCase().includes(q));
  }, [profiles, query]);

  const toggle = (id: string) => {
    const next = selected.includes(id)
      ? selected.filter((x) => x !== id)
      : [...selected, id];
    onChange(stable(next));
  };

  return (
    <div className="card">
      {title ? <div className="h1">{title}</div> : null}
      <div className="row">
        <input
          className="input"
          placeholder="Search profiles"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <button className="btn" onClick={() => onChange(stable(filtered.map((p) => p.id)))}>
          Select All
        </button>
        <button className="btn ghost" onClick={() => onChange([])}>
          Clear
        </button>
      </div>
      <div className="list">
        {filtered.map((p) => (
          <label key={p.id} className="listRow">
            <input type="checkbox" checked={selected.includes(p.id)} onChange={() => toggle(p.id)} />
            <div className="listText">
              <div className="name">{p.name}</div>
              <div className="sub">{p.id}</div>
            </div>
          </label>
        ))}
      </div>
    </div>
  );
}

function stable(arr: string[]) {
  return Array.from(new Set(arr)).sort((a, b) => a.localeCompare(b));
}
