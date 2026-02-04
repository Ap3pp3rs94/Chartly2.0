import React, { useEffect, useMemo, useState } from "react";

type ProfileItem = { id: string; name: string };
type SchemaField = { path: string; type: string; example?: any };
type SchemaResp = {
  profile_id: string;
  name: string;
  fields: SchemaField[];
  join_keys: string[];
  numeric_fields: string[];
};

type CorrelateResp = {
  joined_count: number;
  correlation?: { coefficient: number; p_value: number; interpretation: string };
  preview: Array<{ join_value: string; a_value: any; b_value: any; a_label: string; b_label: string }>;
};

export default function Correlate() {
  const [profiles, setProfiles] = useState<ProfileItem[]>([]);
  const [loadingProfiles, setLoadingProfiles] = useState<boolean>(true);
  const [err, setErr] = useState<string>("");

  const [profileA, setProfileA] = useState<string>("");
  const [profileB, setProfileB] = useState<string>("");
  const [schemaA, setSchemaA] = useState<SchemaResp | null>(null);
  const [schemaB, setSchemaB] = useState<SchemaResp | null>(null);
  const [loadingA, setLoadingA] = useState<boolean>(false);
  const [loadingB, setLoadingB] = useState<boolean>(false);

  const [joinA, setJoinA] = useState<string>("");
  const [joinB, setJoinB] = useState<string>("");
  const [numA, setNumA] = useState<string>("");
  const [numB, setNumB] = useState<string>("");

  const [limit, setLimit] = useState<number>(100);
  const [maxJoined, setMaxJoined] = useState<number>(200);

  const [result, setResult] = useState<CorrelateResp | null>(null);
  const [running, setRunning] = useState<boolean>(false);

  useEffect(() => {
    let alive = true;
    async function loadProfiles() {
      setLoadingProfiles(true);
      setErr("");
      try {
        const res = await fetch("/api/profiles");
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = await res.json();
        const list = Array.isArray(data?.profiles) ? data.profiles : [];
        list.sort((a: ProfileItem, b: ProfileItem) => a.id.localeCompare(b.id));
        if (!alive) return;
        setProfiles(list);
        if (list.length > 0) setProfileA(list[0].id);
        if (list.length > 1) setProfileB(list[1].id);
      } catch (e: any) {
        if (!alive) return;
        setErr(String(e?.message ?? e));
      } finally {
        if (alive) setLoadingProfiles(false);
      }
    }
    loadProfiles();
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!profileA) return;
    let alive = true;
    async function loadSchema() {
      setLoadingA(true);
      try {
        const res = await fetch(`/api/profiles/${encodeURIComponent(profileA)}/schema`);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as SchemaResp;
        if (!alive) return;
        setSchemaA(data);
        setJoinA(data.join_keys?.[0] ?? "");
        setNumA(data.numeric_fields?.[0] ?? "");
      } catch {
        if (!alive) return;
        setSchemaA(null);
      } finally {
        if (alive) setLoadingA(false);
      }
    }
    loadSchema();
    return () => {
      alive = false;
    };
  }, [profileA]);

  useEffect(() => {
    if (!profileB) return;
    let alive = true;
    async function loadSchema() {
      setLoadingB(true);
      try {
        const res = await fetch(`/api/profiles/${encodeURIComponent(profileB)}/schema`);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = (await res.json()) as SchemaResp;
        if (!alive) return;
        setSchemaB(data);
        setJoinB(data.join_keys?.[0] ?? "");
        setNumB(data.numeric_fields?.[0] ?? "");
      } catch {
        if (!alive) return;
        setSchemaB(null);
      } finally {
        if (alive) setLoadingB(false);
      }
    }
    loadSchema();
    return () => {
      alive = false;
    };
  }, [profileB]);

  const canRun = useMemo(() => {
    return !!profileA && !!profileB && !!joinA && !!joinB;
  }, [profileA, profileB, joinA, joinB]);

  async function runCorrelate() {
    if (!canRun) return;
    setRunning(true);
    setResult(null);
    setErr("");
    try {
      const payload = {
        dataset_a: { profile_id: profileA, join_key: joinA, numeric_field: numA },
        dataset_b: { profile_id: profileB, join_key: joinB, numeric_field: numB },
        limit,
        max_joined: maxJoined,
      };
      const res = await fetch("/api/analytics/correlate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = (await res.json()) as CorrelateResp;
      setResult(data);
    } catch (e: any) {
      setErr(String(e?.message ?? e));
    } finally {
      setRunning(false);
    }
  }

  async function exportCSV() {
    if (!canRun) return;
    const payload = {
      dataset_a: { profile_id: profileA, join_key: joinA, numeric_field: numA },
      dataset_b: { profile_id: profileB, join_key: joinB, numeric_field: numB },
      limit,
      max_joined: maxJoined,
    };
    const spec = encodeURIComponent(JSON.stringify(payload));
    window.location.href = `/api/analytics/correlate/export?format=csv&spec=${spec}`;
  }

  return (
    <div className="min-h-screen bg-gray-950 text-white p-8">
      <h1 className="text-3xl font-bold mb-2">Correlate</h1>
      <p className="text-gray-400 mb-8">
        Join two profile result sets by a shared key. Example join keys: location.state_code, location.name, company.cik
      </p>

      {loadingProfiles ? (
        <div className="text-gray-400 mb-6">Loading profiles...</div>
      ) : err ? (
        <div className="text-red-400 mb-6">Error: {err}</div>
      ) : null}

      <div className="grid grid-cols-2 gap-6 mb-6">
        <div className="bg-gray-900 rounded-xl p-6">
          <span className="bg-blue-600 text-white text-sm px-3 py-1 rounded">Dataset A</span>

          <label className="block mt-4 text-gray-400 text-sm">profile_id</label>
          <select
            value={profileA}
            onChange={(e) => setProfileA(e.target.value)}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2 w-full"
          >
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>

          <label className="block mt-4 text-gray-400 text-sm">join key JSON path (A)</label>
          <select
            disabled={!schemaA || loadingA}
            value={joinA}
            onChange={(e) => setJoinA(e.target.value)}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2 w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loadingA && <option>Loading...</option>}
            {schemaA?.join_keys?.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>

          <label className="block mt-4 text-gray-400 text-sm">numeric field (A) for correlation</label>
          <select
            disabled={!schemaA || loadingA}
            value={numA}
            onChange={(e) => setNumA(e.target.value)}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2 w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <option value="">-- optional --</option>
            {schemaA?.numeric_fields?.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
        </div>

        <div className="bg-gray-900 rounded-xl p-6">
          <span className="bg-blue-600 text-white text-sm px-3 py-1 rounded">Dataset B</span>

          <label className="block mt-4 text-gray-400 text-sm">profile_id</label>
          <select
            value={profileB}
            onChange={(e) => setProfileB(e.target.value)}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2 w-full"
          >
            {profiles.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>

          <label className="block mt-4 text-gray-400 text-sm">join key JSON path (B)</label>
          <select
            disabled={!schemaB || loadingB}
            value={joinB}
            onChange={(e) => setJoinB(e.target.value)}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2 w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loadingB && <option>Loading...</option>}
            {schemaB?.join_keys?.map((k) => (
              <option key={k} value={k}>
                {k}
              </option>
            ))}
          </select>

          <label className="block mt-4 text-gray-400 text-sm">numeric field (B) for correlation</label>
          <select
            disabled={!schemaB || loadingB}
            value={numB}
            onChange={(e) => setNumB(e.target.value)}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2 w-full disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <option value="">-- optional --</option>
            {schemaB?.numeric_fields?.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="flex gap-4 items-end mb-8">
        <div>
          <label className="text-gray-400 text-sm">limit per profile (rows)</label>
          <input
            type="number"
            value={limit}
            onChange={(e) => setLimit(Number(e.target.value))}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2"
          />
        </div>
        <div>
          <label className="text-gray-400 text-sm">max joined preview</label>
          <input
            type="number"
            value={maxJoined}
            onChange={(e) => setMaxJoined(Number(e.target.value))}
            className="bg-gray-800 border border-gray-700 text-white rounded px-4 py-2"
          />
        </div>
        <button
          onClick={runCorrelate}
          disabled={!canRun || running}
          className="bg-blue-600 hover:bg-blue-700 px-6 py-2 rounded disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {running ? "Running..." : "Join + Analyze"}
        </button>
        <button onClick={exportCSV} className="bg-gray-700 hover:bg-gray-600 px-6 py-2 rounded">
          Export Joined CSV
        </button>
      </div>

      <div className="bg-gray-900 rounded-xl p-6">
        <h2 className="text-xl font-semibold mb-2">Output</h2>
        <p className="text-gray-400 text-sm mb-4">Joined preview + correlation (if numeric fields provided).</p>

        {result?.correlation && (
          <div className="mb-4 p-4 bg-gray-800 rounded">
            <p>
              Correlation: <strong>{result.correlation.coefficient.toFixed(3)}</strong>
            </p>
            <p className="text-gray-400">{result.correlation.interpretation}</p>
          </div>
        )}

        {running && <p>Running...</p>}
        {!running && !result && <p className="text-gray-500">Run Join + Analyze.</p>}
        {result && (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-gray-400">
                <th className="py-2">join_value</th>
                <th className="py-2">a_value</th>
                <th className="py-2">b_value</th>
              </tr>
            </thead>
            <tbody>
              {result.preview.map((row, idx) => (
                <tr key={`${row.join_value}-${idx}`} className="border-t border-gray-800">
                  <td className="py-2">{row.join_value}</td>
                  <td className="py-2">{String(row.a_value)}</td>
                  <td className="py-2">{String(row.b_value)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
