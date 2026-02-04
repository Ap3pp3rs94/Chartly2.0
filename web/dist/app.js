
/* Chartly Control Plane UI (minimal) */
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

const STORAGE = { apiKey: "chartly_api_key", limit: "chartly_default_limit" };
const state = {
  apiKey: sessionStorage.getItem(STORAGE.apiKey) || "",
  limit: Number(sessionStorage.getItem(STORAGE.limit) || "100"),
  services: { registry: "unknown", aggregator: "unknown", coordinator: "unknown" },
  cache: { profiles: [], fields: new Map() },
};

const ROUTES = [
  { path: "/", label: "Dashboard", render: pageDashboard },
  { path: "/profiles", label: "Profiles", render: pageProfiles },
  { path: "/results", label: "Results", render: pageResults },
  { path: "/correlate", label: "Correlate", render: pageCorrelate },
  { path: "/charts", label: "Charts", render: pageCharts },
  { path: "/settings", label: "Settings", render: pageSettings },
];

boot();

function boot() {
  renderNav();
  wireGlobal();
  route(location.pathname);
  refreshStatus(true).catch(() => {});
}

function renderNav() {
  const nav = $("#nav");
  nav.innerHTML = "";
  for (const r of ROUTES) {
    const a = document.createElement("a");
    a.href = r.path;
    a.textContent = r.label;
    a.setAttribute("data-link", "1");
    nav.appendChild(a);
  }
  setActive(location.pathname);
}

function setActive(path) {
  $$("#nav a").forEach(a => a.classList.toggle("active", a.getAttribute("href") === path));
}

function wireGlobal() {
  document.addEventListener("click", (e) => {
    const a = e.target.closest("a[data-link]");
    if (!a) return;
    const u = new URL(a.href);
    if (u.origin !== location.origin) return;
    e.preventDefault();
    navigate(u.pathname);
  });
  $("#btnRefresh").addEventListener("click", () => route(location.pathname, { force: true }));
  window.addEventListener("popstate", () => route(location.pathname));
}

function navigate(path) {
  if (path === location.pathname) return;
  history.pushState({}, "", path);
  route(path);
}

async function route(path, { force = false } = {}) {
  const r = ROUTES.find(x => x.path === path) || ROUTES[0];
  setActive(r.path);
  const page = $("#page");
  page.innerHTML = skeleton(r.label);
  await refreshStatus(true).catch(() => {});
  try {
    await r.render(page, { force });
  } catch (e) {
    toast("bad", "UI error", msg(e));
    page.innerHTML = errorCard("Render failed", e);
  }
}

/* ---------------- API ---------------- */

function reqId() {
  return (globalThis.crypto && crypto.randomUUID) ? crypto.randomUUID() : ("req-" + Math.random().toString(16).slice(2));
}
function sleep(ms) { return new Promise(r => setTimeout(r, ms)); }

async function fetchTimeout(url, opts = {}, timeoutMs = 20000) {
  const c = new AbortController();
  const t = setTimeout(() => c.abort(), timeoutMs);
  try { return await fetch(url, { ...opts, signal: c.signal }); }
  finally { clearTimeout(t); }
}

async function apiJson(path, opts = {}, { retries = 1, timeoutMs = 20000 } = {}) {
  const id = reqId();
  const headers = new Headers(opts.headers || {});
  headers.set("Accept", "application/json");
  headers.set("X-Request-ID", id);
  const url = path.startsWith("/") ? path : ("/" + path);

  let last = null;
  for (let a = 0; a <= retries; a++) {
    try {
      const res = await fetchTimeout(url, { ...opts, headers }, timeoutMs);
      const text = await res.text();
      let data = null;
      if (text && text.trim()) {
        try { data = JSON.parse(text); } catch { data = { raw: text }; }
      }
      if (!res.ok) {
        const e = new Error((data && data.message) ? data.message : `HTTP ${res.status}`);
        e.status = res.status; e.data = data;
        throw e;
      }
      return data;
    } catch (e) {
      last = e;
      const st = (e && typeof e.status === "number") ? e.status : 0;
      const retryable = st === 0 || st >= 500 || st === 429;
      if (a < retries && retryable) {
        await sleep([800, 1500, 3000][a] || 3000);
        continue;
      }
      throw last;
    }
  }
  throw last || new Error("request_failed");
}
/* ---------------- UI helpers ---------------- */

function esc(s) {
  return String(s ?? "")
    .replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}
function msg(e) { return (e && e.message) ? String(e.message) : String(e); }

function skeleton(title) {
  return `<div class="card"><div class="h1">${esc(title)}</div><p class="hint">Loading</p></div>`;
}
function errorCard(title, err) {
  return `<div class="card"><div class="h1">${esc(title)}</div><pre>${esc(String(err && err.stack ? err.stack : err))}</pre></div>`;
}
function toast(kind, title, text) {
  const host = $("#toasts");
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.innerHTML = `<div class="t">${esc(title)}</div><div class="small muted">${esc(text)}</div>`;
  host.appendChild(el);
  setTimeout(() => { el.style.opacity = "0"; el.style.transform = "translateY(4px)"; setTimeout(() => el.remove(), 220); }, 3500);
}
function badgeClass(v) {
  const s = String(v || "").toLowerCase();
  if (s === "up" || s === "healthy" || s === "ok") return "good";
  if (s === "down" || s === "unhealthy" || s === "error") return "bad";
  return "warn";
}

function setDot(id, val) {
  const el = $(id);
  el.className = "dot " + badgeClass(val);
}

async function refreshStatus(soft) {
  try {
    const st = await apiJson("/api/status", {}, { retries: soft ? 0 : 1, timeoutMs: 8000 });
    const gw = st && st.status ? st.status : "unknown";
    $("#gwStatus").textContent = gw;
    const sv = st && st.services ? st.services : {};
    state.services.registry = sv.registry || "unknown";
    state.services.aggregator = sv.aggregator || "unknown";
    state.services.coordinator = sv.coordinator || "unknown";
    setDot("#dotRegistry", state.services.registry);
    setDot("#dotAggregator", state.services.aggregator);
    setDot("#dotCoordinator", state.services.coordinator);
  } catch {
    $("#gwStatus").textContent = "down";
    setDot("#dotRegistry", "unknown");
    setDot("#dotAggregator", "unknown");
    setDot("#dotCoordinator", "unknown");
  }
}

/* ---------------- Data helpers ---------------- */

async function loadProfiles() {
  try {
    const res = await apiJson("/api/profiles", {}, { retries: 0, timeoutMs: 8000 });
    let arr = [];
    if (Array.isArray(res)) arr = res;
    else if (res && Array.isArray(res.profiles)) arr = res.profiles;
    arr.sort((a, b) => String(a.id).localeCompare(String(b.id)));
    state.cache.profiles = arr;
  } catch {
    state.cache.profiles = [];
  }
  return state.cache.profiles;
}

async function loadFields(profileId) {
  const cached = state.cache.fields.get(profileId);
  if (cached && cached.expiresAt > Date.now()) return cached.data;
  const data = await apiJson(`/api/profiles/${encodeURIComponent(profileId)}/fields`, {}, { retries: 1, timeoutMs: 15000 });
  const expires = Date.now() + 5 * 60 * 1000;
  state.cache.fields.set(profileId, { data, expiresAt: expires });
  return data;
}

function optionList(fields, filterFn) {
  return fields
    .filter(filterFn)
    .sort((a, b) => String(a.label).localeCompare(String(b.label)))
    .map(f => `<option value="${esc(f.path)}">${esc(f.label)}</option>`)
    .join("");
}
/* ---------------- Pages: Dashboard, Profiles, Results ---------------- */

async function pageDashboard(root) {
  const profiles = await loadProfiles();
  let summary = null;
  try { summary = await apiJson("/api/results/summary", {}, { retries: 0, timeoutMs: 8000 }); } catch {}
  root.innerHTML = `
    <div class="card">
      <div class="h1">Dashboard</div>
      <div class="row">
        <span class="badge ${badgeClass(state.services.registry)}">Registry: ${esc(state.services.registry)}</span>
        <span class="badge ${badgeClass(state.services.aggregator)}">Aggregator: ${esc(state.services.aggregator)}</span>
        <span class="badge ${badgeClass(state.services.coordinator)}">Coordinator: ${esc(state.services.coordinator)}</span>
      </div>
      <div class="row" style="margin-top:10px;">
        <span class="badge">Profiles: ${esc(profiles.length)}</span>
        <span class="badge">Results: ${esc(summary && typeof summary.total_results === "number" ? summary.total_results : "n/a")}</span>
      </div>
    </div>
  `;
}

async function pageProfiles(root) {
  const profiles = await loadProfiles();
  root.innerHTML = `
    <div class="card">
      <div class="h1">Profiles</div>
      <div class="row">
        <button class="btn" id="reload">Reload</button>
        <button class="btn" id="setKey">Set API Key</button>
      </div>
      <div class="tableWrap" style="margin-top:10px;">
        <table class="table">
          <thead><tr><th>id</th><th>name</th><th>view</th></tr></thead>
          <tbody>
          ${profiles.map(p => `
            <tr>
              <td class="mono">${esc(p.id || "")}</td>
              <td>${esc(p.name || "")}</td>
              <td><button class="btn" data-view="${esc(p.id || "")}">View</button></td>
            </tr>
          `).join("")}
          </tbody>
        </table>
      </div>
    </div>

    <div class="card">
      <div class="h1">Create</div>
      <textarea id="yaml" class="input" spellcheck="false" placeholder="id: my-profile"></textarea>
      <div class="row" style="margin-top:10px;">
        <button class="btn" id="create">Create</button>
      </div>
      <div class="hint">Session key only.</div>
    </div>

    <div class="card">
      <div class="h1">View</div>
      <div id="viewer" class="small muted">Select a profile.</div>
    </div>
  `;

  $("#reload").addEventListener("click", async () => {
    await loadProfiles();
    toast("good", "Reloaded", `profiles=${state.cache.profiles.length}`);
    route("/profiles", { force: true });
  });
  $("#setKey").addEventListener("click", () => navigate("/settings"));

  root.addEventListener("click", async (e) => {
    const b = e.target.closest("button[data-view]");
    if (!b) return;
    const id = b.getAttribute("data-view");
    const v = $("#viewer");
    v.textContent = "Loading...";
    try {
      const p = await apiJson(`/api/profiles/${encodeURIComponent(id)}`, {}, { retries: 1, timeoutMs: 8000 });
      v.innerHTML = `<pre>${esc(String(p.content || ""))}</pre>`;
    } catch (err) {
      v.textContent = msg(err);
    }
  });

  $("#create").addEventListener("click", async () => {
    if (!state.apiKey) { toast("bad", "Missing key", "Set X-API-Key in Settings."); navigate("/settings"); return; }
    const yaml = $("#yaml").value || "";
    const id = extractId(yaml);
    if (!id) { toast("warn", "Invalid", "YAML must include id."); return; }
    try {
      await apiJson("/api/profiles", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-API-Key": state.apiKey },
        body: JSON.stringify({ id, name: id, version: "0.0.0", content: yaml }),
      });
      toast("good", "Created", id);
      $("#yaml").value = "";
      await loadProfiles();
      route("/profiles", { force: true });
    } catch (e) {
      toast("bad", "Create failed", msg(e));
    }
  });
}

async function pageResults(root) {
  const profiles = await loadProfiles();
  root.innerHTML = `
    <div class="card">
      <div class="h1">Results</div>
      <div class="grid3">
        <div>
          <div class="small muted">profile_id</div>
          <select id="p" class="input">
            <option value="">(any)</option>
            ${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}
          </select>
        </div>
        <div>
          <div class="small muted">drone_id</div>
          <input id="d" class="input" placeholder="drone-123" />
        </div>
        <div>
          <div class="small muted">limit</div>
          <input id="l" class="input" value="${esc(state.limit)}" />
        </div>
      </div>
      <div class="row" style="margin-top:10px;">
        <button class="btn" id="run">Query</button>
        <button class="btn" id="export" disabled>Export CSV</button>
      </div>
    </div>
    <div class="card">
      <div class="h1">Output</div>
      <div id="out" class="small muted">Run a query.</div>
    </div>
  `;

  let lastRows = [];
  $("#run").addEventListener("click", async () => {
    const p = $("#p").value.trim();
    const d = $("#d").value.trim();
    const l = parseInt($("#l").value.trim(), 10) || state.limit || 100;
    const qs = new URLSearchParams();
    if (p) qs.set("profile_id", p);
    if (d) qs.set("drone_id", d);
    qs.set("limit", String(l));
    const box = $("#out");
    box.textContent = "Loading...";
    try {
      const rows = await apiJson(`/api/results?${qs.toString()}`, {}, { retries: 1, timeoutMs: 15000 });
      lastRows = Array.isArray(rows) ? rows : [];
      $("#export").disabled = lastRows.length === 0;
      box.innerHTML = lastRows.length === 0 ? "No results." : renderTable(
        ["timestamp", "profile_id", "drone_id", "data_preview"],
        lastRows.map(r => [
          esc(r.timestamp || ""),
          esc(r.profile_id || ""),
          esc(r.drone_id || ""),
          esc(trunc(JSON.stringify(r.data), 120))
        ])
      );
    } catch (e) {
      box.textContent = msg(e);
    }
  });
  $("#export").addEventListener("click", () => {
    const records = expandRows(lastRows).map(x => x.record);
    const csv = toCsv(records);
    downloadText("results.csv", csv, "text/csv;charset=utf-8");
    toast("good", "Exported", "results.csv downloaded");
  });
}
/* ---------------- Page: Correlate ---------------- */

async function pageCorrelate(root) {
  const profiles = await loadProfiles();
  root.innerHTML = `
    <div class="card">
      <div class="h1">Correlate</div>
      <div class="grid2">
        <div>
          <div class="small muted">Dataset A</div>
          <select id="pa" class="input">${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}</select>
          <div style="height:6px;"></div>
          <select id="ja" class="input" disabled></select>
          <div style="height:6px;"></div>
          <select id="na" class="input" disabled></select>
        </div>
        <div>
          <div class="small muted">Dataset B</div>
          <select id="pb" class="input">${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}</select>
          <div style="height:6px;"></div>
          <select id="jb" class="input" disabled></select>
          <div style="height:6px;"></div>
          <select id="nb" class="input" disabled></select>
        </div>
      </div>
      <div class="row" style="margin-top:10px;">
        <button class="btn" id="go">Join + Analyze</button>
      </div>
    </div>
    <div class="card">
      <div class="h1">Output</div>
      <div id="out" class="small muted">Run Join + Analyze.</div>
    </div>
  `;

  async function loadFor(selProfile, selJoin, selNum) {
    selJoin.disabled = true;
    selNum.disabled = true;
    selJoin.innerHTML = "";
    selNum.innerHTML = "";
    try {
      const data = await loadFields(selProfile.value);
      const fields = Array.isArray(data.fields) ? data.fields : [];
      selJoin.innerHTML = optionList(fields, f => f.type === "string");
      selNum.innerHTML = `<option value="">(optional)</option>` + optionList(fields, f => f.type === "number");
      selJoin.disabled = false;
      selNum.disabled = false;
    } catch {
      selJoin.innerHTML = `<option value="">(unavailable)</option>`;
      selNum.innerHTML = `<option value="">(unavailable)</option>`;
    }
  }

  const pa = $("#pa"), pb = $("#pb"), ja = $("#ja"), jb = $("#jb"), na = $("#na"), nb = $("#nb");
  await loadFor(pa, ja, na);
  await loadFor(pb, jb, nb);
  pa.addEventListener("change", () => loadFor(pa, ja, na));
  pb.addEventListener("change", () => loadFor(pb, jb, nb));

  $("#go").addEventListener("click", async () => {
    const payload = {
      dataset_a: { profile_id: pa.value, join_key: ja.value, numeric_field: na.value || "" },
      dataset_b: { profile_id: pb.value, join_key: jb.value, numeric_field: nb.value || "" },
      limit: state.limit,
      max_joined: 200,
    };
    const out = $("#out");
    out.textContent = "Running...";
    try {
      const res = await apiJson("/api/analytics/correlate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      }, { retries: 1, timeoutMs: 20000 });
      out.innerHTML = `<pre>${esc(JSON.stringify(res, null, 2).slice(0, 2000))}</pre>`;
    } catch (e) {
      out.textContent = msg(e);
    }
  });
}
/* ---------------- Page: Charts ---------------- */

let xType = "number";

async function pageCharts(root) {
  const profiles = await loadProfiles();
  root.innerHTML = `
    <div class="card">
      <div class="h1">Charts</div>
      <div class="grid2">
        <div>
          <div class="small muted">Dataset A</div>
          <select id="pa" class="input">${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}</select>
          <div style="height:6px;"></div>
          <select id="ya" class="input" disabled></select>
        </div>
        <div>
          <div class="small muted">Dataset B</div>
          <select id="pb" class="input">${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}</select>
          <div style="height:6px;"></div>
          <select id="yb" class="input" disabled></select>
        </div>
      </div>
      <div class="row" style="margin-top:10px;">
        <div style="min-width:220px;">
          <div class="small muted">X field</div>
          <select id="xfield" class="input" disabled></select>
        </div>
        <button class="btn" id="render">Render</button>
        <button class="btn" id="export">Export CSV</button>
        <button class="btn" id="reset">Reset Zoom</button>
        <button class="btn link" id="advBtn">Advanced</button>
      </div>
      <div id="adv" class="adv">
        <div class="grid3" style="margin-top:10px;">
          <div>
            <div class="small muted">Group key</div>
            <select id="gkey" class="input" disabled></select>
          </div>
          <div>
            <div class="small muted">Chart type</div>
            <select id="ctype" class="input">
              <option value="line">Line</option>
              <option value="candle">Candlestick</option>
            </select>
          </div>
          <div>
            <div class="small muted">Limit per profile</div>
            <input id="limit" class="input" value="${esc(state.limit)}" />
          </div>
        </div>
        <div class="grid3" style="margin-top:10px;">
          <div>
            <div class="small muted">Point cap</div>
            <input id="cap" class="input" value="5000" />
          </div>
          <div>
            <div class="small muted">Series toggles</div>
            <div id="seriesToggles" class="small muted">Enable group key to show.</div>
          </div>
        </div>
      </div>
    </div>
    <div class="card">
      <div class="canvasWrap"><canvas id="chart"></canvas></div>
      <div id="warn" class="small muted" style="margin-top:6px;"></div>
    </div>
  `;

  const pa = $("#pa"), pb = $("#pb"), ya = $("#ya"), yb = $("#yb"), xf = $("#xfield");
  const gk = $("#gkey"), adv = $("#adv"), advBtn = $("#advBtn");
  const ctype = $("#ctype"), limit = $("#limit"), cap = $("#cap");
  const seriesToggles = $("#seriesToggles");

  const selected = { hidden: new Set(), zoom: null };
  let lastSeries = [];

  async function loadFieldsFor(profileId, ySel, xSel, gSel) {
    ySel.disabled = true; xSel.disabled = true; gSel.disabled = true;
    ySel.innerHTML = ""; xSel.innerHTML = ""; gSel.innerHTML = "";
    try {
      const data = await loadFields(profileId);
      const fields = Array.isArray(data.fields) ? data.fields : [];
      const num = fields.filter(f => f.type === "number");
      const str = fields.filter(f => f.type === "string");
      const xCandidates = fields.filter(f => /year|date|time|timestamp/i.test(f.label) || f.type === "number" || f.type === "string");
      ySel.innerHTML = optionList(num, () => true);
      xSel.innerHTML = optionList(xCandidates, () => true);
      gSel.innerHTML = `<option value="">(none)</option>` + optionList(str, () => true);
      ySel.disabled = false; xSel.disabled = false; gSel.disabled = false;
    } catch {
      ySel.innerHTML = `<option value="">(unavailable)</option>`;
      xSel.innerHTML = `<option value="">(unavailable)</option>`;
      gSel.innerHTML = `<option value="">(unavailable)</option>`;
    }
  }

  await loadFieldsFor(pa.value, ya, xf, gk);
  await loadFieldsFor(pb.value, yb, xf, gk);
  pa.addEventListener("change", () => loadFieldsFor(pa.value, ya, xf, gk));
  pb.addEventListener("change", () => loadFieldsFor(pb.value, yb, xf, gk));

  advBtn.addEventListener("click", () => adv.classList.toggle("open"));

  $("#render").addEventListener("click", async () => {
    const xPath = xf.value;
    const yA = ya.value;
    const yB = yb.value;
    const groupPath = gk.value;
    const maxCap = Math.max(1, Math.min(5000, parseInt(cap.value, 10) || 5000));
    const lim = parseInt(limit.value, 10) || state.limit || 100;
    const outWarn = $("#warn");
    outWarn.textContent = "";
    seriesToggles.textContent = groupPath ? "" : "Enable group key to show.";
    selected.hidden.clear();

    const [rowsA, rowsB] = await Promise.all([
      apiJson(`/api/results?profile_id=${encodeURIComponent(pa.value)}&limit=${encodeURIComponent(String(lim))}`, {}, { retries: 1, timeoutMs: 20000 }),
      apiJson(`/api/results?profile_id=${encodeURIComponent(pb.value)}&limit=${encodeURIComponent(String(lim))}`, {}, { retries: 1, timeoutMs: 20000 }),
    ]);

    const recA = expandRows(Array.isArray(rowsA) ? rowsA : []).map(x => x.record);
    const recB = expandRows(Array.isArray(rowsB) ? rowsB : []).map(x => x.record);

    const points = [];
    pushPoints(points, recA, xPath, yA, groupPath, "A");
    pushPoints(points, recB, xPath, yB, groupPath, "B");

    if (points.length === 0) {
      toast("bad", "No plottable points", "Check X/Y field selection.");
      return;
    }

    points.sort((a, b) => (a.x - b.x) || String(a.series).localeCompare(String(b.series)));
    if (points.length > maxCap) {
      points.length = maxCap;
      outWarn.textContent = `Truncated to ${maxCap} points.`;
    }

    const series = groupSeries(points);
    lastSeries = series;
    buildSeriesToggles(series);
    renderChart(series, ctype.value);
  });

  $("#export").addEventListener("click", () => {
    if (!lastSeries.length) return;
    const rows = [];
    for (const s of lastSeries) {
      if (selected.hidden.has(s.key)) continue;
      for (const p of s.points) rows.push({ series: s.key, x: p.rawX, y: p.y });
    }
    const csv = toCsv(rows);
    downloadText("chart.csv", csv, "text/csv;charset=utf-8");
    toast("good", "Exported", "chart.csv downloaded");
  });

  $("#reset").addEventListener("click", () => {
    selected.zoom = null;
    if (lastSeries.length) renderChart(lastSeries, ctype.value);
  });

  function buildSeriesToggles(series) {
    if (!gk.value) return;
    seriesToggles.innerHTML = series.map(s => `
      <label class="small"><input type="checkbox" data-series="${esc(s.key)}" checked> ${esc(s.key)}</label>
    `).join("<br/>");
    seriesToggles.querySelectorAll("input[type=checkbox]").forEach(cb => {
      cb.addEventListener("change", () => {
        const key = cb.getAttribute("data-series");
        if (cb.checked) selected.hidden.delete(key); else selected.hidden.add(key);
        renderChart(lastSeries, ctype.value);
      });
    });
  }

  function renderChart(series, type) {
    const canvas = $("#chart");
    const ctx = canvas.getContext("2d");
    const rect = canvas.getBoundingClientRect();
    canvas.width = Math.max(600, Math.floor(rect.width * devicePixelRatio));
    canvas.height = Math.floor(rect.height * devicePixelRatio);
    ctx.scale(devicePixelRatio, devicePixelRatio);

    const pad = { l: 50, r: 20, t: 20, b: 30 };
    const w = rect.width, h = rect.height;
    const plotW = w - pad.l - pad.r, plotH = h - pad.t - pad.b;

    const all = [];
    for (const s of series) {
      if (selected.hidden.has(s.key)) continue;
      all.push(...s.points);
    }
    if (all.length === 0) { ctx.clearRect(0, 0, w, h); return; }

    let minX = Math.min(...all.map(p => p.x));
    let maxX = Math.max(...all.map(p => p.x));
    let minY = Math.min(...all.map(p => p.y));
    let maxY = Math.max(...all.map(p => p.y));
    if (selected.zoom) { minX = selected.zoom[0]; maxX = selected.zoom[1]; }
    if (minY === maxY) { minY -= 1; maxY += 1; }

    const xToPx = x => pad.l + (x - minX) / (maxX - minX) * plotW;
    const yToPx = y => pad.t + plotH - (y - minY) / (maxY - minY) * plotH;

    ctx.clearRect(0, 0, w, h);
    ctx.strokeStyle = "rgba(255,255,255,0.08)";
    ctx.lineWidth = 1;
    for (let i = 0; i <= 5; i++) {
      const yy = pad.t + (plotH / 5) * i;
      ctx.beginPath(); ctx.moveTo(pad.l, yy); ctx.lineTo(pad.l + plotW, yy); ctx.stroke();
    }
    for (let i = 0; i <= 5; i++) {
      const xx = pad.l + (plotW / 5) * i;
      ctx.beginPath(); ctx.moveTo(xx, pad.t); ctx.lineTo(xx, pad.t + plotH); ctx.stroke();
    }

    if (type === "candle") {
      const candles = groupCandles(all);
      const wbar = Math.max(2, plotW / Math.max(10, candles.length));
      for (const c of candles) {
        const x = xToPx(c.x);
        const openY = yToPx(c.open);
        const closeY = yToPx(c.close);
        const highY = yToPx(c.high);
        const lowY = yToPx(c.low);
        const up = c.close >= c.open;
        ctx.strokeStyle = up ? "#3bd671" : "#ff5c7a";
        ctx.fillStyle = up ? "rgba(59,214,113,.5)" : "rgba(255,92,122,.5)";
        ctx.beginPath(); ctx.moveTo(x, highY); ctx.lineTo(x, lowY); ctx.stroke();
        const y = Math.min(openY, closeY);
        const hgt = Math.max(2, Math.abs(closeY - openY));
        ctx.fillRect(x - wbar / 3, y, wbar / 1.5, hgt);
      }
    } else {
      for (const s of series) {
        if (selected.hidden.has(s.key)) continue;
        ctx.strokeStyle = colorFor(s.key);
        ctx.lineWidth = 2;
        ctx.beginPath();
        let started = false;
        for (const p of s.points) {
          if (p.x < minX || p.x > maxX) continue;
          const px = xToPx(p.x);
          const py = yToPx(p.y);
          if (!started) { ctx.moveTo(px, py); started = true; } else { ctx.lineTo(px, py); }
        }
        ctx.stroke();
      }
    }

    let dragging = false;
    let dragStart = null;
    canvas.onwheel = (e) => {
      e.preventDefault();
      const dir = Math.sign(e.deltaY);
      const span = maxX - minX;
      const factor = dir > 0 ? 1.1 : 0.9;
      const mid = minX + span / 2;
      const newSpan = span * factor;
      selected.zoom = [mid - newSpan / 2, mid + newSpan / 2];
      renderChart(series, type);
    };
    canvas.onmousedown = (e) => { dragging = true; dragStart = e.offsetX; };
    canvas.onmouseup = (e) => {
      if (!dragging) return;
      dragging = false;
      const end = e.offsetX;
      if (Math.abs(end - dragStart) < 4) return;
      const x1 = minX + (Math.min(dragStart, end) - pad.l) / plotW * (maxX - minX);
      const x2 = minX + (Math.max(dragStart, end) - pad.l) / plotW * (maxX - minX);
      if (x2 > x1) selected.zoom = [x1, x2];
      renderChart(series, type);
    };
  }

  function groupSeries(points) {
    const map = new Map();
    for (const p of points) {
      if (!map.has(p.series)) map.set(p.series, []);
      map.get(p.series).push(p);
    }
    const out = [];
    for (const [k, arr] of map.entries()) {
      arr.sort((a, b) => (a.x - b.x));
      out.push({ key: k, points: arr });
    }
    out.sort((a, b) => String(a.key).localeCompare(String(b.key)));
    return out;
  }

  function groupCandles(points) {
    const buckets = new Map();
    for (const p of points) {
      const key = xType === "time" ? dayBucket(p.x) : p.x;
      if (!buckets.has(key)) buckets.set(key, []);
      buckets.get(key).push(p);
    }
    const out = [];
    for (const [k, arr] of buckets.entries()) {
      arr.sort((a, b) => a.x - b.x);
      const open = arr[0].y;
      const close = arr[arr.length - 1].y;
      const high = Math.max(...arr.map(a => a.y));
      const low = Math.min(...arr.map(a => a.y));
      out.push({ x: k, open, close, high, low });
    }
    out.sort((a, b) => a.x - b.x);
    return out;
  }

  function dayBucket(ms) {
    const d = new Date(ms);
    return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
  }
}
/* ---------------- Page: Charts ---------------- */

let xType = "number";

async function pageCharts(root) {
  const profiles = await loadProfiles();
  root.innerHTML = `
    <div class="card">
      <div class="h1">Charts</div>
      <div class="grid2">
        <div>
          <div class="small muted">Dataset A</div>
          <select id="pa" class="input">${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}</select>
          <div style="height:6px;"></div>
          <select id="ya" class="input" disabled></select>
        </div>
        <div>
          <div class="small muted">Dataset B</div>
          <select id="pb" class="input">${profiles.map(p => `<option value="${esc(p.id)}">${esc(p.id)}</option>`).join("")}</select>
          <div style="height:6px;"></div>
          <select id="yb" class="input" disabled></select>
        </div>
      </div>
      <div class="row" style="margin-top:10px;">
        <div style="min-width:220px;">
          <div class="small muted">X field</div>
          <select id="xfield" class="input" disabled></select>
        </div>
        <button class="btn" id="render">Render</button>
        <button class="btn" id="export">Export CSV</button>
        <button class="btn" id="reset">Reset Zoom</button>
        <button class="btn link" id="advBtn">Advanced</button>
      </div>
      <div id="adv" class="adv">
        <div class="grid3" style="margin-top:10px;">
          <div>
            <div class="small muted">Group key</div>
            <select id="gkey" class="input" disabled></select>
          </div>
          <div>
            <div class="small muted">Chart type</div>
            <select id="ctype" class="input">
              <option value="line">Line</option>
              <option value="candle">Candlestick</option>
            </select>
          </div>
          <div>
            <div class="small muted">Limit per profile</div>
            <input id="limit" class="input" value="${esc(state.limit)}" />
          </div>
        </div>
        <div class="grid3" style="margin-top:10px;">
          <div>
            <div class="small muted">Point cap</div>
            <input id="cap" class="input" value="5000" />
          </div>
          <div>
            <div class="small muted">Series toggles</div>
            <div id="seriesToggles" class="small muted">Enable group key to show.</div>
          </div>
        </div>
      </div>
    </div>
    <div class="card">
      <div class="canvasWrap"><canvas id="chart"></canvas></div>
      <div id="warn" class="small muted" style="margin-top:6px;"></div>
    </div>
  `;

  const pa = $("#pa"), pb = $("#pb"), ya = $("#ya"), yb = $("#yb"), xf = $("#xfield");
  const gk = $("#gkey"), adv = $("#adv"), advBtn = $("#advBtn");
  const ctype = $("#ctype"), limit = $("#limit"), cap = $("#cap");
  const seriesToggles = $("#seriesToggles");

  const selected = { hidden: new Set(), zoom: null };
  let lastSeries = [];

  async function loadFieldsFor(profileId, ySel, xSel, gSel) {
    ySel.disabled = true; xSel.disabled = true; gSel.disabled = true;
    ySel.innerHTML = ""; xSel.innerHTML = ""; gSel.innerHTML = "";
    try {
      const data = await loadFields(profileId);
      const fields = Array.isArray(data.fields) ? data.fields : [];
      const num = fields.filter(f => f.type === "number");
      const str = fields.filter(f => f.type === "string");
      const xCandidates = fields.filter(f => /year|date|time|timestamp/i.test(f.label) || f.type === "number" || f.type === "string");
      ySel.innerHTML = optionList(num, () => true);
      xSel.innerHTML = optionList(xCandidates, () => true);
      gSel.innerHTML = `<option value="">(none)</option>` + optionList(str, () => true);
      ySel.disabled = false; xSel.disabled = false; gSel.disabled = false;
    } catch {
      ySel.innerHTML = `<option value="">(unavailable)</option>`;
      xSel.innerHTML = `<option value="">(unavailable)</option>`;
      gSel.innerHTML = `<option value="">(unavailable)</option>`;
    }
  }

  await loadFieldsFor(pa.value, ya, xf, gk);
  await loadFieldsFor(pb.value, yb, xf, gk);
  pa.addEventListener("change", () => loadFieldsFor(pa.value, ya, xf, gk));
  pb.addEventListener("change", () => loadFieldsFor(pb.value, yb, xf, gk));

  advBtn.addEventListener("click", () => adv.classList.toggle("open"));

  $("#render").addEventListener("click", async () => {
    const xPath = xf.value;
    const yA = ya.value;
    const yB = yb.value;
    const groupPath = gk.value;
    const maxCap = Math.max(1, Math.min(5000, parseInt(cap.value, 10) || 5000));
    const lim = parseInt(limit.value, 10) || state.limit || 100;
    const outWarn = $("#warn");
    outWarn.textContent = "";
    seriesToggles.textContent = groupPath ? "" : "Enable group key to show.";
    selected.hidden.clear();

    const [rowsA, rowsB] = await Promise.all([
      apiJson(`/api/results?profile_id=${encodeURIComponent(pa.value)}&limit=${encodeURIComponent(String(lim))}`, {}, { retries: 1, timeoutMs: 20000 }),
      apiJson(`/api/results?profile_id=${encodeURIComponent(pb.value)}&limit=${encodeURIComponent(String(lim))}`, {}, { retries: 1, timeoutMs: 20000 }),
    ]);

    const recA = expandRows(Array.isArray(rowsA) ? rowsA : []).map(x => x.record);
    const recB = expandRows(Array.isArray(rowsB) ? rowsB : []).map(x => x.record);

    const points = [];
    pushPoints(points, recA, xPath, yA, groupPath, "A");
    pushPoints(points, recB, xPath, yB, groupPath, "B");

    if (points.length === 0) {
      toast("bad", "No plottable points", "Check X/Y field selection.");
      return;
    }

    points.sort((a, b) => (a.x - b.x) || String(a.series).localeCompare(String(b.series)));
    if (points.length > maxCap) {
      points.length = maxCap;
      outWarn.textContent = `Truncated to ${maxCap} points.`;
    }

    const series = groupSeries(points);
    lastSeries = series;
    buildSeriesToggles(series);
    renderChart(series, ctype.value);
  });

  $("#export").addEventListener("click", () => {
    if (!lastSeries.length) return;
    const rows = [];
    for (const s of lastSeries) {
      if (selected.hidden.has(s.key)) continue;
      for (const p of s.points) rows.push({ series: s.key, x: p.rawX, y: p.y });
    }
    const csv = toCsv(rows);
    downloadText("chart.csv", csv, "text/csv;charset=utf-8");
    toast("good", "Exported", "chart.csv downloaded");
  });

  $("#reset").addEventListener("click", () => {
    selected.zoom = null;
    if (lastSeries.length) renderChart(lastSeries, ctype.value);
  });

  function buildSeriesToggles(series) {
    if (!gk.value) return;
    seriesToggles.innerHTML = series.map(s => `
      <label class="small"><input type="checkbox" data-series="${esc(s.key)}" checked> ${esc(s.key)}</label>
    `).join("<br/>");
    seriesToggles.querySelectorAll("input[type=checkbox]").forEach(cb => {
      cb.addEventListener("change", () => {
        const key = cb.getAttribute("data-series");
        if (cb.checked) selected.hidden.delete(key); else selected.hidden.add(key);
        renderChart(lastSeries, ctype.value);
      });
    });
  }

  function renderChart(series, type) {
    const canvas = $("#chart");
    const ctx = canvas.getContext("2d");
    const rect = canvas.getBoundingClientRect();
    canvas.width = Math.max(600, Math.floor(rect.width * devicePixelRatio));
    canvas.height = Math.floor(rect.height * devicePixelRatio);
    ctx.scale(devicePixelRatio, devicePixelRatio);

    const pad = { l: 50, r: 20, t: 20, b: 30 };
    const w = rect.width, h = rect.height;
    const plotW = w - pad.l - pad.r, plotH = h - pad.t - pad.b;

    const all = [];
    for (const s of series) {
      if (selected.hidden.has(s.key)) continue;
      all.push(...s.points);
    }
    if (all.length === 0) { ctx.clearRect(0, 0, w, h); return; }

    let minX = Math.min(...all.map(p => p.x));
    let maxX = Math.max(...all.map(p => p.x));
    let minY = Math.min(...all.map(p => p.y));
    let maxY = Math.max(...all.map(p => p.y));
    if (selected.zoom) { minX = selected.zoom[0]; maxX = selected.zoom[1]; }
    if (minY === maxY) { minY -= 1; maxY += 1; }

    const xToPx = x => pad.l + (x - minX) / (maxX - minX) * plotW;
    const yToPx = y => pad.t + plotH - (y - minY) / (maxY - minY) * plotH;

    ctx.clearRect(0, 0, w, h);
    ctx.strokeStyle = "rgba(255,255,255,0.08)";
    ctx.lineWidth = 1;
    for (let i = 0; i <= 5; i++) {
      const yy = pad.t + (plotH / 5) * i;
      ctx.beginPath(); ctx.moveTo(pad.l, yy); ctx.lineTo(pad.l + plotW, yy); ctx.stroke();
    }
    for (let i = 0; i <= 5; i++) {
      const xx = pad.l + (plotW / 5) * i;
      ctx.beginPath(); ctx.moveTo(xx, pad.t); ctx.lineTo(xx, pad.t + plotH); ctx.stroke();
    }

    if (type === "candle") {
      const candles = groupCandles(all);
      const wbar = Math.max(2, plotW / Math.max(10, candles.length));
      for (const c of candles) {
        const x = xToPx(c.x);
        const openY = yToPx(c.open);
        const closeY = yToPx(c.close);
        const highY = yToPx(c.high);
        const lowY = yToPx(c.low);
        const up = c.close >= c.open;
        ctx.strokeStyle = up ? "#3bd671" : "#ff5c7a";
        ctx.fillStyle = up ? "rgba(59,214,113,.5)" : "rgba(255,92,122,.5)";
        ctx.beginPath(); ctx.moveTo(x, highY); ctx.lineTo(x, lowY); ctx.stroke();
        const y = Math.min(openY, closeY);
        const hgt = Math.max(2, Math.abs(closeY - openY));
        ctx.fillRect(x - wbar / 3, y, wbar / 1.5, hgt);
      }
    } else {
      for (const s of series) {
        if (selected.hidden.has(s.key)) continue;
        ctx.strokeStyle = colorFor(s.key);
        ctx.lineWidth = 2;
        ctx.beginPath();
        let started = false;
        for (const p of s.points) {
          if (p.x < minX || p.x > maxX) continue;
          const px = xToPx(p.x);
          const py = yToPx(p.y);
          if (!started) { ctx.moveTo(px, py); started = true; } else { ctx.lineTo(px, py); }
        }
        ctx.stroke();
      }
    }

    let dragging = false;
    let dragStart = null;
    canvas.onwheel = (e) => {
      e.preventDefault();
      const dir = Math.sign(e.deltaY);
      const span = maxX - minX;
      const factor = dir > 0 ? 1.1 : 0.9;
      const mid = minX + span / 2;
      const newSpan = span * factor;
      selected.zoom = [mid - newSpan / 2, mid + newSpan / 2];
      renderChart(series, type);
    };
    canvas.onmousedown = (e) => { dragging = true; dragStart = e.offsetX; };
    canvas.onmouseup = (e) => {
      if (!dragging) return;
      dragging = false;
      const end = e.offsetX;
      if (Math.abs(end - dragStart) < 4) return;
      const x1 = minX + (Math.min(dragStart, end) - pad.l) / plotW * (maxX - minX);
      const x2 = minX + (Math.max(dragStart, end) - pad.l) / plotW * (maxX - minX);
      if (x2 > x1) selected.zoom = [x1, x2];
      renderChart(series, type);
    };
  }

  function groupSeries(points) {
    const map = new Map();
    for (const p of points) {
      if (!map.has(p.series)) map.set(p.series, []);
      map.get(p.series).push(p);
    }
    const out = [];
    for (const [k, arr] of map.entries()) {
      arr.sort((a, b) => (a.x - b.x));
      out.push({ key: k, points: arr });
    }
    out.sort((a, b) => String(a.key).localeCompare(String(b.key)));
    return out;
  }

  function groupCandles(points) {
    const buckets = new Map();
    for (const p of points) {
      const key = xType === "time" ? dayBucket(p.x) : p.x;
      if (!buckets.has(key)) buckets.set(key, []);
      buckets.get(key).push(p);
    }
    const out = [];
    for (const [k, arr] of buckets.entries()) {
      arr.sort((a, b) => a.x - b.x);
      const open = arr[0].y;
      const close = arr[arr.length - 1].y;
      const high = Math.max(...arr.map(a => a.y));
      const low = Math.min(...arr.map(a => a.y));
      out.push({ x: k, open, close, high, low });
    }
    out.sort((a, b) => a.x - b.x);
    return out;
  }

  function dayBucket(ms) {
    const d = new Date(ms);
    return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
  }
}
/* ---------------- Page: Settings ---------------- */

async function pageSettings(root) {
  root.innerHTML = `
    <div class="card">
      <div class="h1">Settings</div>
      <div class="grid2">
        <div>
          <div class="small muted">X-API-Key (session only)</div>
          <input id="key" class="input" value="${esc(state.apiKey)}" />
          <div class="hint">Session only.</div>
        </div>
        <div>
          <div class="small muted">Default limit</div>
          <input id="lim" class="input" value="${esc(state.limit)}" />
        </div>
      </div>
      <div class="row" style="margin-top:10px;">
        <button class="btn" id="save">Save</button>
        <button class="btn" id="clear">Clear</button>
      </div>
    </div>
  `;
  $("#save").addEventListener("click", () => {
    const k = $("#key").value.trim();
    const lim = parseInt($("#lim").value.trim(), 10) || 100;
    state.apiKey = k;
    state.limit = lim;
    sessionStorage.setItem(STORAGE.apiKey, k);
    sessionStorage.setItem(STORAGE.limit, String(lim));
    toast("good", "Saved", "Session updated");
    navigate("/");
  });
  $("#clear").addEventListener("click", () => {
    sessionStorage.removeItem(STORAGE.apiKey);
    sessionStorage.removeItem(STORAGE.limit);
    state.apiKey = "";
    state.limit = 100;
    toast("warn", "Cleared", "Session cleared");
    route("/settings", { force: true });
  });
}

/* ---------------- Utils ---------------- */

function extractId(yaml) {
  const lines = String(yaml || "").split(/\r?\n/);
  for (const line of lines) {
    const m = line.match(/^\s*id\s*:\s*(.+?)\s*$/i);
    if (m) return String(m[1]).replace(/^["']|["']$/g, "").trim();
  }
  return "";
}

function trunc(s, n) {
  const t = String(s || "");
  if (t.length <= n) return t;
  return t.slice(0, n) + "…";
}

function renderTable(headers, rows) {
  const thead = headers.map(h => `<th>${esc(h)}</th>`).join("");
  const tbody = rows.map(cols => `<tr>${cols.map(c => `<td>${c}</td>`).join("")}</tr>`).join("");
  return `<div class="tableWrap"><table class="table"><thead><tr>${thead}</tr></thead><tbody>${tbody}</tbody></table></div>`;
}

function expandRows(rows) {
  const out = [];
  for (const r of rows) {
    let data = r.data;
    if (typeof data === "string") { try { data = JSON.parse(data); } catch { data = null; } }
    const items = Array.isArray(data) ? data : (data && typeof data === "object" ? [data] : []);
    for (const rec of items) out.push({ record: rec });
  }
  return out;
}

function getPath(obj, path) {
  const p = String(path || "").trim();
  if (!p) return undefined;
  const norm = p.replace(/\[(\d+)\]/g, ".$1");
  const parts = norm.split(".").filter(Boolean);
  let cur = obj;
  for (const part of parts) {
    if (cur == null) return undefined;
    const isIndex = /^[0-9]+$/.test(part);
    if (isIndex && Array.isArray(cur)) cur = cur[Number(part)];
    else if (typeof cur === "object") cur = cur[part];
    else return undefined;
  }
  return cur;
}

function pushPoints(out, records, xPath, yPath, groupPath, label) {
  for (const rec of records) {
    const xv = getPath(rec, xPath);
    const yv = getPath(rec, yPath);
    const x = parseX(xv);
    const y = toNumber(yv);
    if (x == null || y == null) continue;
    const g = groupPath ? getPath(rec, groupPath) : null;
    const key = g != null ? String(g) : label;
    out.push({ x: x.value, rawX: x.raw, y, series: key });
  }
}

function parseX(v) {
  if (v == null) return null;
  if (typeof v === "number" && Number.isFinite(v)) { xType = "number"; return { value: v, raw: v }; }
  const s = String(v).trim();
  if (!s) return null;
  const num = Number(s);
  if (Number.isFinite(num)) { xType = "number"; return { value: num, raw: s }; }
  const ms = Date.parse(s);
  if (!Number.isNaN(ms)) { xType = "time"; return { value: ms, raw: s }; }
  return null;
}

function toNumber(x) {
  if (x == null) return null;
  if (typeof x === "number" && Number.isFinite(x)) return x;
  const n = Number(String(x).replaceAll(",", "").trim());
  return Number.isFinite(n) ? n : null;
}

function colorFor(key) {
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) % 360;
  return `hsl(${h},70%,60%)`;
}

function flatten(obj, prefix = "", out = {}) {
  if (obj == null) return out;
  if (Array.isArray(obj)) { out[prefix || "value"] = JSON.stringify(obj); return out; }
  if (typeof obj !== "object") { out[prefix || "value"] = String(obj); return out; }
  for (const k of Object.keys(obj)) {
    const v = obj[k];
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === "object" && !Array.isArray(v)) flatten(v, key, out);
    else if (Array.isArray(v)) out[key] = JSON.stringify(v);
    else out[key] = v == null ? "" : String(v);
  }
  return out;
}

function toCsv(rows) {
  const flat = rows.map(r => flatten(r));
  const keys = Array.from(new Set(flat.flatMap(o => Object.keys(o)))).sort((a, b) => a.localeCompare(b));
  const escCsv = (s) => {
    const v = String(s ?? "");
    if (/[,"\n]/.test(v)) return `"${v.replaceAll('"', '""')}"`;
    return v;
  };
  const head = keys.map(escCsv).join(",");
  const lines = flat.map(o => keys.map(k => escCsv(o[k] ?? "")).join(","));
  return [head, ...lines].join("\n") + "\n";
}

function downloadText(name, text, mime) {
  const blob = new Blob([text], { type: mime || "text/plain;charset=utf-8" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}
