
const $ = (sel, root=document) => root.querySelector(sel);
const $$ = (sel, root=document) => Array.from(root.querySelectorAll(sel));

const STORAGE = { apiKey: "chartly_api_key", limit: "chartly_default_limit" };

const state = {
  apiKey: sessionStorage.getItem(STORAGE.apiKey) || "",
  limit: Number(sessionStorage.getItem(STORAGE.limit) || "100"),
  services: { registry: "unknown", aggregator: "unknown", coordinator: "unknown", reporter: "unknown" },
  counts: { profiles: "", drones: "", results: "" },
  lastReq: { id: "", info: "No requests yet" },
  cache: { profiles: [], drones: [], summary: null }
};

const ROUTES = [
  { path: "/", label: "Dashboard", render: pageDashboard },
  { path: "/charts", label: "Charts", render: pageCharts },
  { path: "/profiles", label: "Profiles", render: pageProfiles },
  { path: "/drones", label: "Drones", render: pageDrones },
  { path: "/results", label: "Results", render: pageResults },
  { path: "/runs", label: "Runs", render: pageRuns },
  { path: "/correlate", label: "Correlate", render: pageCorrelate },
  { path: "/settings", label: "Settings", render: pageSettings }
];

boot();

function boot(){
  renderNav();
  wireGlobal();
  route(location.pathname, { soft:false });
  refreshSidebar({ soft:true }).catch(()=>{});
}

function renderNav(){
  const nav = $("#nav");
  nav.innerHTML = "";
  for (const r of ROUTES){
    if (r.path === "/settings") continue;
    const a = document.createElement("a");
    a.href = r.path;
    a.textContent = r.label;
    a.setAttribute("data-link","1");
    nav.appendChild(a);
  }
  setActive(location.pathname);
}

function setActive(path){
  $$("#nav a").forEach(a=>a.classList.toggle("active", a.getAttribute("href") === path));
}

function wireGlobal(){
  document.addEventListener("click", (e)=>{
    const a = e.target.closest("a[data-link]");
    if (!a) return;
    const u = new URL(a.href);
    if (u.origin !== location.origin) return;
    e.preventDefault();
    navigate(u.pathname);
  });
  $("#btnRefresh").addEventListener("click", ()=>route(location.pathname, { soft:false, force:true }));
}

function navigate(path){
  if (path === location.pathname) return;
  history.pushState({}, "", path);
  route(path, { soft:false });
}

window.addEventListener("popstate", ()=>route(location.pathname, { soft:true }));

async function route(path, { soft=true, force=false } = {}){
  const r = ROUTES.find(x=>x.path===path) || ROUTES[0];
  setActive(r.path);
  const page = $("#page");
  page.innerHTML = skeleton(r.label);
  await refreshSidebar({ soft:true }).catch(()=>{});
  try { await r.render(page, { force, soft }); }
  catch (err){ toast("bad","UI Error", msg(err)); page.innerHTML = errorCard("Render failed", err); }
}

function reqId(){
  return (globalThis.crypto && crypto.randomUUID) ? crypto.randomUUID() : ("req-"+Math.random().toString(16).slice(2)+"-"+Math.random().toString(16).slice(2));
}

function sleep(ms){ return new Promise(r=>setTimeout(r,ms)); }

async function fetchTimeout(url, opts={}, timeoutMs=30000){
  const c = new AbortController();
  const t = setTimeout(()=>c.abort(), timeoutMs);
  try { return await fetch(url, { ...opts, signal:c.signal }); }
  finally { clearTimeout(t); }
}

async function apiJson(path, opts={}, { retries=2, timeoutMs=20000 } = {}){
  const id = reqId();
  const headers = new Headers(opts.headers || {});
  headers.set("Accept","application/json");
  headers.set("X-Request-ID", id);
  const url = path.startsWith("/") ? path : ("/"+path);
  let last = null;
  for (let a=0; a<=retries; a++){
    try{
      const res = await fetchTimeout(url, { ...opts, headers }, timeoutMs);
      const text = await res.text();
      state.lastReq = { id, info: `${opts.method || "GET"} ${url} -> ${res.status}` };
      renderLastReq();
      let data = null;
      if (text && text.trim().length){ try { data = JSON.parse(text); } catch { data = { raw:text }; } }
      if (!res.ok){
        const e = new Error((data && data.message) ? data.message : `HTTP ${res.status}`);
        e.status = res.status; e.data = data; throw e;
      }
      return data;
    }catch(e){
      last = e;
      const st = (e && typeof e.status === "number") ? e.status : 0;
      const retryable = st === 0 || st >= 500 || st === 429;
      if (a < retries && retryable){ await sleep([800,1500,3000][a] || 3000); continue; }
      throw last;
    }
  }
  throw last || new Error("request_failed");
}

function esc(s){
  return String(s ?? "")
    .replaceAll("&","&amp;")
    .replaceAll("<","&lt;")
    .replaceAll(">","&gt;")
    .replaceAll('"',"&quot;")
    .replaceAll("'","&#39;");
}

function msg(e){ return (e && e.message) ? String(e.message) : String(e); }

function toast(kind, title, text){
  const host = $("#toasts");
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.innerHTML = `<div class="t">${esc(title)}</div><div class="small muted">${esc(text)}</div>`;
  host.appendChild(el);
  setTimeout(()=>{
    el.style.opacity = "0";
    el.style.transform = "translateY(4px)";
    setTimeout(()=>el.remove(), 220);
  }, 3500);
}

function skeleton(title){
  return `<div class="card"><div class="h1">${esc(title)}</div><p class="hint">Loading</p></div>`;
}

function errorCard(title, err){
  const detail = esc(String(err && err.stack ? err.stack : err));
  return `<div class="card"><div class="h1">${esc(title)}</div><p class="hint">Try Refresh. If it persists, check gateway logs.</p><pre>${detail}</pre></div>`;
}

function badgeClass(v){
  const s = String(v||"").toLowerCase();
  if (s==="up"||s==="healthy"||s==="ok") return "good";
  if (s==="down"||s==="unhealthy"||s==="error") return "bad";
  if (s==="unknown"||s==="") return "";
  return "warn";
}

function renderLastReq(){
  $("#lastReqId").textContent = state.lastReq.id || "";
  $("#lastReqInfo").textContent = state.lastReq.info || "";
}

function setSvc(id, value){
  const el = $(id);
  el.textContent = value ?? "unknown";
  el.className = `v ${badgeClass(value)}`;
}

function normalizeServiceValue(v){
  if (!v) return "unknown";
  if (typeof v === "string") return v;
  if (typeof v === "object"){
    if (v.status) return v.status;
    if (v.ok !== undefined) return v.ok ? "up" : "down";
  }
  return "unknown";
}
async function refreshSidebar({ soft=true } = {}){
  try {
    const st = await apiJson("/api/status", {}, { retries: soft ? 0 : 1, timeoutMs: 8000 });
    const gw = st && st.status ? st.status : "unknown";
    $("#gwBadge").textContent = gw;
    $("#gwBadge").className = `badge ${badgeClass(gw)}`;
    const sv = (st && st.services) ? st.services : {};
    state.services.registry = normalizeServiceValue(sv.registry);
    state.services.aggregator = normalizeServiceValue(sv.aggregator);
    state.services.coordinator = normalizeServiceValue(sv.coordinator);
    state.services.reporter = normalizeServiceValue(sv.reporter);
    setSvc("#svcRegistry", state.services.registry);
    setSvc("#svcAggregator", state.services.aggregator);
    setSvc("#svcCoordinator", state.services.coordinator);
    setSvc("#svcReporter", state.services.reporter);
  } catch {
    try {
      const h = await apiJson("/health", {}, { retries:0, timeoutMs:8000 });
      const gw = h && h.status ? h.status : "unknown";
      $("#gwBadge").textContent = gw;
      $("#gwBadge").className = `badge ${badgeClass(gw)}`;
    } catch {
      $("#gwBadge").textContent = "down";
      $("#gwBadge").className = "badge bad";
      setSvc("#svcRegistry","unknown");
      setSvc("#svcAggregator","unknown");
      setSvc("#svcCoordinator","unknown");
      setSvc("#svcReporter","unknown");
      if (!soft) toast("bad","Gateway Down","Could not reach /api/status or /health");
      return;
    }
  }

  try {
    const profiles = await apiJson("/api/profiles", {}, { retries: soft?0:1, timeoutMs: 8000 });
    if (Array.isArray(profiles)) state.cache.profiles = profiles.slice().sort((a,b)=>String(a.id).localeCompare(String(b.id)));
    state.counts.profiles = state.cache.profiles.length;
  } catch {}

  try {
    const drones = await apiJson("/api/drones", {}, { retries: soft?0:1, timeoutMs: 8000 });
    if (Array.isArray(drones)) state.cache.drones = drones.slice().sort((a,b)=>String(a.id).localeCompare(String(b.id)));
    state.counts.drones = state.cache.drones.length;
  } catch {}

  try {
    const sum = await apiJson("/api/results/summary", {}, { retries: soft?0:1, timeoutMs: 8000 });
    state.cache.summary = sum;
    if (sum && typeof sum.total_results === "number") state.counts.results = sum.total_results;
  } catch {}

  $("#countProfiles").textContent = state.counts.profiles ?? "";
  $("#countDrones").textContent = state.counts.drones ?? "";
  $("#countResults").textContent = state.counts.results ?? "";
}

async function pageDashboard(root){
  await refreshSidebar({ soft:false });
  const sum = state.cache.summary;
  root.innerHTML = `
    <div class="card">
      <div class="h1">Dashboard</div>
      <p class="hint">Health, counts, and quick actions.</p>
      <div class="row">
        <span class="badge ${badgeClass($("#gwBadge").textContent)}">Gateway: ${esc($("#gwBadge").textContent)}</span>
        <span class="badge ${badgeClass(state.services.registry)}">Registry: ${esc(state.services.registry)}</span>
        <span class="badge ${badgeClass(state.services.aggregator)}">Aggregator: ${esc(state.services.aggregator)}</span>
        <span class="badge ${badgeClass(state.services.coordinator)}">Coordinator: ${esc(state.services.coordinator)}</span>
        <span class="badge ${badgeClass(state.services.reporter)}">Reporter: ${esc(state.services.reporter)}</span>
      </div>
      <div class="hr"></div>
      <div class="grid3">
        <div class="panel"><div class="panelTitle">Profiles</div><div class="h1">${esc(state.counts.profiles)}</div><div class="small muted">Definitions drones execute.</div><div class="row" style="margin-top:10px;"><button class="btn" id="goProfiles">Open</button></div></div>
        <div class="panel"><div class="panelTitle">Drones</div><div class="h1">${esc(state.counts.drones)}</div><div class="small muted">Active drones seen recently.</div><div class="row" style="margin-top:10px;"><button class="btn" id="goDrones">Open</button></div></div>
        <div class="panel"><div class="panelTitle">Results</div><div class="h1">${esc(state.counts.results)}</div><div class="small muted">Stored outputs (summary).</div><div class="row" style="margin-top:10px;"><button class="btn" id="goResults">Open</button></div></div>
      </div>
    </div>
    <div class="card">
      <div class="h1">Charts</div>
      <p class="hint">Stock-style charting with zoom, crosshair, and export.</p>
      <div class="row"><button class="btn" id="goCharts">Open Charts</button></div>
      <div class="hr"></div>
      <div class="small muted">${sum ? esc(JSON.stringify(sum, null, 2)).slice(0, 900) : "Summary not available yet."}</div>
    </div>
  `;
  $("#goProfiles").addEventListener("click", ()=>navigate("/profiles"));
  $("#goDrones").addEventListener("click", ()=>navigate("/drones"));
  $("#goResults").addEventListener("click", ()=>navigate("/results"));
  $("#goCharts").addEventListener("click", ()=>navigate("/charts"));
}
async function pageCharts(root){
  await refreshSidebar({ soft:false });
  const profiles = state.cache.profiles || [];
  if (!profiles.length){
    root.innerHTML = `<div class="card"><div class="h1">Charts</div><p class="hint">Profiles list is empty. Add profiles or ensure /api/profiles is available.</p></div>`;
    return;
  }

  root.innerHTML = `
    <div class="card">
      <div class="h1">Charts</div>
      <p class="hint">Stock-style line/candlestick chart with zoom, crosshair, series toggles, and CSV export.</p>
      <div class="grid2">
        <div class="panel">
          <div class="panelTitle">Dataset A</div>
          <div class="small muted">profile_id</div>
          <select id="pA" class="input">${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select>
          <div style="height:10px;"></div>
          <div class="small muted">X path (time)</div>
          <input id="xPathA" class="input" value="employment.year" />
          <div style="height:10px;"></div>
          <div class="small muted">Y path (numeric)</div>
          <input id="yPathA" class="input" value="employment.rate" />
          <div style="height:10px;"></div>
          <div class="small muted">Group key (optional)</div>
          <input id="gPathA" class="input" placeholder="location.state_code" />
        </div>

        <div class="panel">
          <div class="panelTitle">Dataset B</div>
          <div class="small muted">profile_id</div>
          <select id="pB" class="input">${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select>
          <div style="height:10px;"></div>
          <div class="small muted">X path (time)</div>
          <input id="xPathB" class="input" value="population.year" />
          <div style="height:10px;"></div>
          <div class="small muted">Y path (numeric)</div>
          <input id="yPathB" class="input" value="population.total" />
          <div style="height:10px;"></div>
          <div class="small muted">Group key (optional)</div>
          <input id="gPathB" class="input" placeholder="location.state_code" />
        </div>
      </div>

      <div class="grid3" style="margin-top:12px;">
        <div>
          <div class="small muted">limit per profile</div>
          <input id="limit" class="input" value="${esc(state.limit)}" />
        </div>
        <div>
          <div class="small muted">chart type</div>
          <select id="chartType" class="input">
            <option value="line">Line</option>
            <option value="candlestick">Candlestick</option>
          </select>
        </div>
        <div>
          <div class="small muted">actions</div>
          <div class="row">
            <button class="btn" id="btnLoad">Load + Render</button>
            <button class="btn" id="btnReset">Reset Zoom</button>
            <button class="btn" id="btnExport" disabled>Export CSV</button>
          </div>
        </div>
      </div>

      <div class="hr"></div>
      <div class="legend" id="legend"></div>
    </div>

    <div class="card">
      <div class="h1">Chart</div>
      <p class="hint">Mouse wheel zooms X-axis. Drag to select a range. Hover for crosshair tooltip.</p>
      <div class="chartWrap" id="chartWrap">
        <canvas id="chart" class="chartCanvas" width="1400" height="420"></canvas>
        <div id="chartOverlay" class="chartOverlay"></div>
        <div id="chartTooltip" class="chartTooltip" style="display:none;"></div>
      </div>
      <div class="small muted" id="chartNote" style="margin-top:8px;"></div>
    </div>
  `;

  if ($("#pB").options.length > 1) $("#pB").selectedIndex = 1;

  const chart = new StockChart($("#chart"), $("#chartOverlay"), $("#chartTooltip"));
  let lastSeries = [];

  $("#btnLoad").addEventListener("click", async ()=>{
    const lim = parseInt($("#limit").value.trim(),10) || state.limit || 100;
    const type = $("#chartType").value;
    const confA = { id: $("#pA").value.trim(), x: $("#xPathA").value.trim(), y: $("#yPathA").value.trim(), g: $("#gPathA").value.trim() };
    const confB = { id: $("#pB").value.trim(), x: $("#xPathB").value.trim(), y: $("#yPathB").value.trim(), g: $("#gPathB").value.trim() };
    $("#chartNote").textContent = "Loading";
    try{
      const [rowsA, rowsB] = await Promise.all([
        apiJson(`/api/results?profile_id=${encodeURIComponent(confA.id)}&limit=${encodeURIComponent(String(lim))}`),
        apiJson(`/api/results?profile_id=${encodeURIComponent(confB.id)}&limit=${encodeURIComponent(String(lim))}`)
      ]);
      const seriesA = extractSeries(confA, Array.isArray(rowsA)?rowsA:[]);
      const seriesB = extractSeries(confB, Array.isArray(rowsB)?rowsB:[]);
      const allSeries = [...seriesA, ...seriesB];
      allSeries.sort((a,b)=>a.key.localeCompare(b.key));
      const capped = capSeries(allSeries, 5000);
      lastSeries = capped.series;
      renderLegend($("#legend"), lastSeries, chart);
      chart.setData(lastSeries, { type });
      chart.render();
      $("#btnExport").disabled = lastSeries.length === 0;
      $("#chartNote").textContent = capped.truncated ? `Truncated to 5000 points (from ${capped.total}).` : `Series: ${lastSeries.length}`;
      toast("good","Rendered", `series=${lastSeries.length}`);
    }catch(e){
      $("#chartNote").textContent = "";
      toast("bad","Chart load failed", msg(e));
    }
  });

  $("#btnReset").addEventListener("click", ()=>{ chart.resetZoom(); chart.render(); });

  $("#btnExport").addEventListener("click", ()=>{
    const rows = chart.exportVisible();
    const csv = toCsv(rows);
    downloadText("chart_points.csv", csv, "text/csv;charset=utf-8");
    toast("good","Exported", `rows=${rows.length}`);
  });
}
async function pageProfiles(root){
  if (!state.cache.profiles.length) await refreshSidebar({ soft:false });
  const profiles = state.cache.profiles;
  root.innerHTML = `
    <div class="card">
      <div class="h1">Profiles</div>
      <p class="hint">View YAML. Create requires API key in Settings.</p>
      <div class="row"><button class="btn" id="reload">Reload</button><button class="btn" id="openSettings">Set API Key</button></div>
      <div class="hr"></div>
      ${profiles.length===0 ? `<div class="small muted">No profiles found.</div>` : `
      <div class="tableWrap"><table class="table"><thead><tr><th>id</th><th>name</th><th>version</th><th>actions</th></tr></thead><tbody>
        ${profiles.map(p=>`<tr><td class="mono">${esc(p.id||"")}</td><td>${esc(p.name||"")}</td><td class="mono">${esc(p.version||"")}</td><td><button class="btn" data-view="${esc(p.id||"")}">View</button></td></tr>`).join("")}
      </tbody></table></div>`}
    </div>
    <div class="card">
      <div class="h1">View / Create</div>
      <div class="grid2">
        <div class="panel"><div class="panelTitle">Selected Profile</div><div id="viewer" class="small muted">Select a profile to view its YAML.</div></div>
        <div class="panel">
          <div class="panelTitle">Create (POST /api/profiles)</div>
          <textarea id="yaml" class="input" spellcheck="false" placeholder="id: my-profile"></textarea>
          <div class="row" style="margin-top:10px;"><button class="btn" id="extract">Extract</button><button class="btn" id="create">Create</button></div>
          <div class="small muted" style="margin-top:10px;">Extracted: <span class="mono" id="meta"></span></div>
        </div>
      </div>
    </div>
  `;
  $("#reload").addEventListener("click", async ()=>{ await refreshSidebar({ soft:false }); toast("good","Reloaded", "Profiles refreshed"); route("/profiles", { force:true, soft:true }); });
  $("#openSettings").addEventListener("click", ()=>navigate("/settings"));
  root.addEventListener("click", async (e)=>{ const b = e.target.closest("button[data-view]"); if (!b) return; await viewProfile(b.getAttribute("data-view")); });
  $("#extract").addEventListener("click", ()=>{ const m = extractMeta($("#yaml").value); $("#meta").textContent = `id=${m.id||"?"} name=${m.name||"?"} version=${m.version||"?"}`; toast("good","Extracted", $("#meta").textContent); });
  $("#create").addEventListener("click", async ()=>{
    if (!state.apiKey){ toast("bad","Missing API Key","Set X-API-Key in Settings."); navigate("/settings"); return; }
    const yaml = $("#yaml").value || ""; const m = extractMeta(yaml);
    if (!yaml.trim() || !m.id){ toast("warn","Invalid YAML","YAML must include id."); return; }
    try{ await apiJson("/api/profiles", { method:"POST", headers:{ "Content-Type":"application/json", "X-API-Key": state.apiKey }, body: JSON.stringify({ id:m.id, name:m.name||m.id, version:m.version||"0.0.0", content: yaml }) }); toast("good","Created", m.id); $("#yaml").value=""; $("#meta").textContent=""; await refreshSidebar({ soft:false }); route("/profiles", { force:true, soft:true }); }catch(e){ toast("bad","Create failed", msg(e)); }
  });
}

async function viewProfile(id){
  const v = $("#viewer");
  v.innerHTML = `<div class="small muted">Loading <span class="mono">${esc(id)}</span></div>`;
  try{
    const p = await apiJson(`/api/profiles/${encodeURIComponent(id)}`);
    const yaml = p && p.content ? String(p.content) : "";
    v.innerHTML = `<div class="panelTitle">${esc(p.name||p.id||id)}</div><div class="small muted">id=<span class="mono">${esc(p.id||id)}</span> version=<span class="mono">${esc(p.version||"")}</span></div><div class="hr"></div><pre>${esc(yaml)}</pre>`;
  }catch(e){ v.innerHTML = `<pre>${esc(msg(e))}</pre>`; }
}

function extractMeta(yaml){
  const out = { id:"", name:"", version:"" };
  const lines = String(yaml||"").split(/\r?\n/);
  for (const line of lines){
    const m = line.match(/^\s*(id|name|version)\s*:\s*(.+?)\s*$/i);
    if (!m) continue;
    const k = m[1].toLowerCase();
    const v = String(m[2]).replace(/^['"]|['"]$/g,"").trim();
    if (k==="id" && !out.id) out.id = v;
    if (k==="name" && !out.name) out.name = v;
    if (k==="version" && !out.version) out.version = v;
    if (out.id && out.name && out.version) break;
  }
  return out;
}

async function pageDrones(root){
  await refreshSidebar({ soft:false });
  let stats = null; try{ stats = await apiJson("/api/drones/stats", {}, { retries:1, timeoutMs:8000 }); }catch{}
  const drones = state.cache.drones || [];
  root.innerHTML = `
    <div class="card">
      <div class="h1">Drones</div>
      <p class="hint">Active drones are those with recent heartbeats.</p>
      <div class="row"><span class="badge">listed=${esc(drones.length)}</span><span class="badge">${stats?`total=${stats.total} active=${stats.active} offline=${stats.offline}`:"stats: planned"}</span><button class="btn" id="reload">Reload</button></div>
      <div class="hr"></div>
      ${drones.length===0 ? `<div class="small muted">No active drones.</div>` : `
      <div class="tableWrap"><table class="table"><thead><tr><th>id</th><th>status</th><th>last_heartbeat</th><th>registered_at</th><th>profiles</th></tr></thead><tbody>
        ${drones.map(d=>`<tr><td class="mono">${esc(d.id||"")}</td><td><span class="badge ${badgeClass(d.status)}">${esc(d.status||"")}</span></td><td class="mono">${esc(d.last_heartbeat||"")}</td><td class="mono">${esc(d.registered_at||"")}</td><td class="mono">${esc(Array.isArray(d.assigned_profiles)?d.assigned_profiles.length:0)}</td></tr>`).join("")}
      </tbody></table></div>`}
    </div>
  `;
  $("#reload").addEventListener("click", async ()=>{ await refreshSidebar({ soft:false }); toast("good","Reloaded", `drones=${state.cache.drones.length}`); route("/drones", { force:true, soft:true }); });
}

async function pageResults(root){
  await refreshSidebar({ soft:false });
  const profiles = state.cache.profiles || [];
  root.innerHTML = `
    <div class="card">
      <div class="h1">Results</div>
      <p class="hint">Query stored results and inspect rows. Export CSV from expanded record data.</p>
      <div class="grid3">
        <div><div class="small muted">drone_id (optional)</div><input id="qDrone" class="input" placeholder="drone-123" /></div>
        <div><div class="small muted">profile_id (optional)</div><select id="qProfile" class="input"><option value="">(any)</option>${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select></div>
        <div><div class="small muted">limit</div><input id="qLimit" class="input" value="${esc(state.limit)}" /></div>
      </div>
      <div class="row" style="margin-top:12px;"><button class="btn" id="btnQuery">Run Query</button><button class="btn" id="btnSummary">Load Summary</button><button class="btn" id="btnExport" disabled>Export CSV</button></div>
    </div>
    <div class="card"><div class="h1">Summary</div><p class="hint">/api/results/summary</p><div id="sumBox" class="panel"><div class="small muted">Not loaded.</div></div></div>
    <div class="card"><div class="h1">Query Output</div><p class="hint">Click Inspect to view a row.</p><div id="outBox" class="panel"><div class="small muted">Run a query above.</div></div></div>
  `;
  let lastRows = [];
  $("#btnSummary").addEventListener("click", async ()=>{ const box = $("#sumBox"); box.innerHTML = `<div class="small muted">Loading</div>`; try{ const sum = await apiJson("/api/results/summary", {}, { retries:1, timeoutMs:12000 }); box.innerHTML = `<pre>${esc(JSON.stringify(sum, null, 2))}</pre>`; }catch(e){ box.innerHTML = `<pre>${esc(msg(e))}</pre>`; } });
  $("#btnQuery").addEventListener("click", async ()=>{
    const drone = $("#qDrone").value.trim(); const profile = $("#qProfile").value.trim(); const limit = parseInt($("#qLimit").value.trim(),10) || state.limit || 100;
    const qs = new URLSearchParams(); if (drone) qs.set("drone_id", drone); if (profile) qs.set("profile_id", profile); qs.set("limit", String(limit));
    const box = $("#outBox"); box.innerHTML = `<div class="small muted">Querying</div>`;
    try{ const rows = await apiJson(`/api/results?${qs.toString()}`, {}, { retries:1, timeoutMs:20000 }); lastRows = Array.isArray(rows)?rows:[]; $("#btnExport").disabled = lastRows.length===0; box.innerHTML = `
      <div class="small muted">rows=${esc(lastRows.length)}</div><div class="hr"></div>
      ${lastRows.length===0 ? `<div class="small muted">No results.</div>` : `<div class="tableWrap"><table class="table"><thead><tr><th>timestamp</th><th>drone_id</th><th>profile_id</th><th>inspect</th></tr></thead><tbody>${lastRows.map((r,i)=>`<tr><td class="mono">${esc(r.timestamp||"")}</td><td class="mono">${esc(r.drone_id||"")}</td><td class="mono">${esc(r.profile_id||"")}</td><td><button class="btn" data-inspect="${i}">Inspect</button></td></tr>`).join("")}</tbody></table></div>`}
      <div class="hr"></div><div id="inspectBox" class="panel"><div class="small muted">Click Inspect on a row.</div></div>
    `;
    box.addEventListener("click", (e)=>{ const b = e.target.closest("button[data-inspect]"); if (!b) return; const i = parseInt(b.getAttribute("data-inspect"),10); renderInspector($("#inspectBox"), lastRows[i]); });
    toast("good","Query complete", `rows=${lastRows.length}`);
    }catch(e){ box.innerHTML = `<pre>${esc(msg(e))}</pre>`; toast("bad","Query failed", msg(e)); }
  });
  $("#btnExport").addEventListener("click", ()=>{ if (!lastRows.length) return; const records = expandRows(lastRows, true).map(x=>x.record); const csv = toCsv(records); downloadText("results.csv", csv, "text/csv;charset=utf-8"); toast("good","Exported", "results.csv downloaded"); });
}
async function pageRuns(root){
  root.innerHTML = `<div class="card"><div class="h1">Runs</div><p class="hint">If /api/runs exists, you will see run history here.</p><div class="row"><button class="btn" id="reload">Reload</button><input id="limit" class="input" style="max-width:160px;" value="${esc(state.limit)}" /></div><div class="hr"></div><div id="runsBox" class="panel"><div class="small muted">Press Reload.</div></div></div>`;
  $("#reload").addEventListener("click", async ()=>{
    const lim = parseInt($("#limit").value.trim(),10) || state.limit || 100;
    const box = $("#runsBox"); box.innerHTML = `<div class="small muted">Loading</div>`;
    try{ const runs = await apiJson(`/api/runs?limit=${encodeURIComponent(String(lim))}`, {}, { retries:1, timeoutMs:12000 }); const arr = Array.isArray(runs)?runs:[]; arr.sort((a,b)=>String(a.started_at||"").localeCompare(String(b.started_at||"")) || String(a.run_id||"").localeCompare(String(b.run_id||""))); box.innerHTML = arr.length===0 ? `<div class="small muted">No runs returned.</div>` : `<div class="tableWrap"><table class="table"><thead><tr><th>started_at</th><th>run_id</th><th>profile_id</th><th>drone_id</th><th>status</th><th>rows</th></tr></thead><tbody>${arr.map(r=>`<tr><td class="mono">${esc(r.started_at||"")}</td><td class="mono">${esc(r.run_id||"")}</td><td class="mono">${esc(r.profile_id||"")}</td><td class="mono">${esc(r.drone_id||"")}</td><td><span class="badge ${badgeClass(r.status)}">${esc(r.status||"")}</span></td><td class="mono">${esc(r.rows_out??"")}</td></tr>`).join("")}</tbody></table></div>`; toast("good","Runs loaded", `count=${arr.length}`); }catch(e){ box.innerHTML = `<div class="small muted">/api/runs not available (planned) or error:</div><pre>${esc(msg(e))}</pre>`; toast("warn","Runs planned", "Endpoint not available or returned error."); }
  });
}

async function pageCorrelate(root){
  await refreshSidebar({ soft:false });
  const profiles = state.cache.profiles || [];
  root.innerHTML = `
    <div class="card">
      <div class="h1">Correlate</div>
      <p class="hint">Join two profiles by a join key path, then compute Pearson on numeric paths (optional).</p>
      <div class="grid2">
        <div class="panel"><div class="panelTitle">Dataset A</div><div class="small muted">profile_id</div><select id="aId" class="input">${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select><div style="height:10px;"></div><div class="small muted">join key path (A)</div><input id="aJoin" class="input" value="location.state_code" /><div style="height:10px;"></div><div class="small muted">numeric path (A) optional</div><input id="aNum" class="input" placeholder="population.total" /></div>
        <div class="panel"><div class="panelTitle">Dataset B</div><div class="small muted">profile_id</div><select id="bId" class="input">${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select><div style="height:10px;"></div><div class="small muted">join key path (B)</div><input id="bJoin" class="input" value="location.state_code" /><div style="height:10px;"></div><div class="small muted">numeric path (B) optional</div><input id="bNum" class="input" placeholder="employment.rate" /></div>
      </div>
      <div class="grid3" style="margin-top:12px;"><div><div class="small muted">limit per profile</div><input id="lim" class="input" value="${esc(state.limit)}" /></div><div><div class="small muted">preview rows (max 500)</div><input id="preview" class="input" value="200" /></div><div><div class="small muted">actions</div><div class="row"><button class="btn" id="go">Join + Analyze</button><button class="btn" id="exp" disabled>Export CSV</button></div></div></div>
    </div>
    <div class="card"><div class="h1">Output</div><p class="hint">Preview + correlation (if both numeric paths provided).</p><div id="out" class="panel"><div class="small muted">Run Join + Analyze.</div></div></div>
  `;
  if ($("#bId").options.length > 1) $("#bId").selectedIndex = 1;
  let joined = [];
  $("#go").addEventListener("click", async ()=>{
    const aId = $("#aId").value.trim(); const bId = $("#bId").value.trim();
    const aJoin = $("#aJoin").value.trim(); const bJoin = $("#bJoin").value.trim();
    const aNum = $("#aNum").value.trim(); const bNum = $("#bNum").value.trim();
    const lim = parseInt($("#lim").value.trim(),10) || state.limit || 100;
    const prev = Math.max(1, Math.min(500, parseInt($("#preview").value.trim(),10) || 200));
    const out = $("#out"); out.innerHTML = `<div class="small muted">Fetching results</div>`; $("#exp").disabled = true; joined = [];
    try{
      const [aRows,bRows] = await Promise.all([
        apiJson(`/api/results?profile_id=${encodeURIComponent(aId)}&limit=${encodeURIComponent(String(lim))}`, {}, { retries:1, timeoutMs:20000 }),
        apiJson(`/api/results?profile_id=${encodeURIComponent(bId)}&limit=${encodeURIComponent(String(lim))}`, {}, { retries:1, timeoutMs:20000 })
      ]);
      const A = expandRows(Array.isArray(aRows)?aRows:[], false).map(x=>x.record);
      const B = expandRows(Array.isArray(bRows)?bRows:[], false).map(x=>x.record);
      joined = joinTwo(A,B,aJoin,bJoin);
      const corr = (aNum && bNum) ? pearson(joined, aNum, bNum) : null;
      out.innerHTML = `
        <div class="small muted">A=${esc(A.length)} B=${esc(B.length)} joined=${esc(joined.length)}</div>
        <div class="hr"></div>
        ${corr ? `<div class="row"><span class="badge">pearson_r: <span class="mono">${esc(corr.r.toFixed(6))}</span></span><span class="badge">n: <span class="mono">${esc(corr.n)}</span></span></div>` : `<div class="small muted">Provide numeric paths for correlation (optional).</div>`}
        <div class="hr"></div>
        ${joined.length===0 ? `<div class="small muted">No join matches.</div>` : `<div class="tableWrap"><table class="table"><thead><tr><th>join_key</th><th>A</th><th>B</th></tr></thead><tbody>${joined.slice(0,prev).map(j=>`<tr><td class="mono">${esc(j.key)}</td><td class="mono">${esc(trunc(JSON.stringify(j.a),220))}</td><td class="mono">${esc(trunc(JSON.stringify(j.b),220))}</td></tr>`).join("")}</tbody></table></div>`}
      `;
      $("#exp").disabled = joined.length===0;
      toast("good","Join complete", `joined=${joined.length}`);
    }catch(e){ out.innerHTML = `<pre>${esc(msg(e))}</pre>`; toast("bad","Join failed", msg(e)); }
  });
  $("#exp").addEventListener("click", ()=>{ if (!joined.length) return; const rows = joined.map(j=>({ join_key:j.key, a:j.a, b:j.b })); const csv = toCsv(rows); downloadText("joined.csv", csv, "text/csv;charset=utf-8"); toast("good","Exported", "joined.csv downloaded"); });
}

async function pageSettings(root){
  root.innerHTML = `<div class="card"><div class="h1">Settings</div><p class="hint">API key stored only for this tab/session.</p><div class="grid2"><div><div class="small muted">X-API-Key</div><input id="key" class="input" value="${esc(state.apiKey)}" /></div><div><div class="small muted">Default limit</div><input id="lim" class="input" value="${esc(state.limit)}" /></div></div><div class="row" style="margin-top:12px;"><button class="btn" id="save">Save</button><button class="btn" id="clear">Clear Session</button></div></div>`;
  $("#save").addEventListener("click", ()=>{ const k = $("#key").value.trim(); const lim = parseInt($("#lim").value.trim(),10) || 100; state.apiKey = k; state.limit = lim; sessionStorage.setItem(STORAGE.apiKey, k); sessionStorage.setItem(STORAGE.limit, String(lim)); toast("good","Saved", "Session settings updated."); navigate("/"); });
  $("#clear").addEventListener("click", ()=>{ sessionStorage.removeItem(STORAGE.apiKey); sessionStorage.removeItem(STORAGE.limit); state.apiKey=""; state.limit=100; toast("warn","Cleared", "Session cleared."); route("/settings", { force:true, soft:true }); });
}
class StockChart{
  constructor(canvas, overlay, tooltip){
    this.canvas = canvas;
    this.ctx = canvas.getContext("2d");
    this.overlay = overlay;
    this.tooltip = tooltip;
    this.series = [];
    this.type = "line";
    this.zoom = null;
    this.hidden = new Set();
    this.drag = null;
    this.canvas.addEventListener("mousemove", (e)=>this.onMove(e));
    this.canvas.addEventListener("mouseleave", ()=>this.onLeave());
    this.canvas.addEventListener("wheel", (e)=>this.onWheel(e));
    this.canvas.addEventListener("mousedown", (e)=>this.onDown(e));
    window.addEventListener("mouseup", (e)=>this.onUp(e));
    window.addEventListener("mousemove", (e)=>this.onDrag(e));
  }

  setData(series, { type="line" } = {}){
    this.series = series || [];
    this.type = type;
  }

  toggleSeries(key){
    if (this.hidden.has(key)) this.hidden.delete(key); else this.hidden.add(key);
    this.render();
  }

  resetZoom(){ this.zoom = null; }

  exportVisible(){
    const { minX, maxX } = this.getXRange();
    const rows = [];
    for (const s of this.series){
      if (this.hidden.has(s.key)) continue;
      for (const p of s.points){
        if (p.x < minX || p.x > maxX) continue;
        rows.push({ series: s.key, time: p.x, value: p.y });
      }
    }
    rows.sort((a,b)=>a.time-b.time || a.series.localeCompare(b.series));
    return rows;
  }

  getXRange(){
    let minX = Infinity, maxX = -Infinity;
    for (const s of this.series){
      if (this.hidden.has(s.key)) continue;
      for (const p of s.points){
        if (p.x < minX) minX = p.x;
        if (p.x > maxX) maxX = p.x;
      }
    }
    if (!isFinite(minX) || !isFinite(maxX)) { minX = 0; maxX = 1; }
    if (this.zoom){ minX = this.zoom.min; maxX = this.zoom.max; }
    if (minX === maxX){ maxX = minX + 1; }
    return { minX, maxX };
  }

  getYRange(minX, maxX){
    let minY = Infinity, maxY = -Infinity;
    for (const s of this.series){
      if (this.hidden.has(s.key)) continue;
      for (const p of s.points){
        if (p.x < minX || p.x > maxX) continue;
        if (p.y < minY) minY = p.y;
        if (p.y > maxY) maxY = p.y;
      }
    }
    if (!isFinite(minY) || !isFinite(maxY)){ minY = 0; maxY = 1; }
    if (minY === maxY){ maxY = minY + 1; }
    return { minY, maxY };
  }

  render(){
    const ctx = this.ctx;
    const w = this.canvas.width;
    const h = this.canvas.height;
    ctx.clearRect(0,0,w,h);

    const pad = { l:60, r:20, t:20, b:40 };
    const { minX, maxX } = this.getXRange();
    const { minY, maxY } = this.getYRange(minX, maxX);

    ctx.strokeStyle = "rgba(255,255,255,0.06)";
    ctx.lineWidth = 1;
    for (let i=0;i<=5;i++){
      const y = pad.t + (h-pad.t-pad.b) * (i/5);
      ctx.beginPath(); ctx.moveTo(pad.l, y); ctx.lineTo(w-pad.r, y); ctx.stroke();
    }
    for (let i=0;i<=5;i++){
      const x = pad.l + (w-pad.l-pad.r) * (i/5);
      ctx.beginPath(); ctx.moveTo(x, pad.t); ctx.lineTo(x, h-pad.b); ctx.stroke();
    }

    ctx.fillStyle = "rgba(255,255,255,0.65)";
    ctx.font = "12px " + getComputedStyle(document.body).fontFamily;
    for (let i=0;i<=5;i++){
      const y = pad.t + (h-pad.t-pad.b) * (i/5);
      const v = maxY - (maxY-minY)*(i/5);
      ctx.fillText(v.toFixed(2), 6, y+4);
    }

    if (this.type === "candlestick"){
      for (const s of this.series){ if (this.hidden.has(s.key)) continue; this.drawCandles(ctx, s, pad, minX, maxX, minY, maxY); }
    } else {
      for (const s of this.series){ if (this.hidden.has(s.key)) continue; this.drawLine(ctx, s, pad, minX, maxX, minY, maxY); }
    }

    if (this.drag){
      const x0 = Math.min(this.drag.x0, this.drag.x1);
      const x1 = Math.max(this.drag.x0, this.drag.x1);
      ctx.fillStyle = "rgba(78,161,255,0.15)";
      ctx.fillRect(x0, pad.t, x1-x0, h-pad.t-pad.b);
      ctx.strokeStyle = "rgba(78,161,255,0.6)";
      ctx.strokeRect(x0, pad.t, x1-x0, h-pad.t-pad.b);
    }

    this.scale = { pad, minX, maxX, minY, maxY, w, h };
  }

  drawLine(ctx, s, pad, minX, maxX, minY, maxY){
    const color = s.color;
    ctx.strokeStyle = color; ctx.lineWidth = 2;
    let started = false;
    for (const p of s.points){
      if (p.x < minX || p.x > maxX) continue;
      const x = mapX(p.x, pad, minX, maxX, this.canvas.width);
      const y = mapY(p.y, pad, minY, maxY, this.canvas.height);
      if (!started){ ctx.beginPath(); ctx.moveTo(x,y); started=true; }
      else ctx.lineTo(x,y);
    }
    if (started) ctx.stroke();
  }

  drawCandles(ctx, s, pad, minX, maxX, minY, maxY){
    const buckets = bucketOHLC(s.points, 50, minX, maxX);
    const width = (this.canvas.width - pad.l - pad.r) / Math.max(1, buckets.length);
    for (const c of buckets){
      const x = mapX(c.x, pad, minX, maxX, this.canvas.width);
      const open = mapY(c.open, pad, minY, maxY, this.canvas.height);
      const close = mapY(c.close, pad, minY, maxY, this.canvas.height);
      const high = mapY(c.high, pad, minY, maxY, this.canvas.height);
      const low = mapY(c.low, pad, minY, maxY, this.canvas.height);
      const up = c.close >= c.open;
      ctx.strokeStyle = s.color;
      ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(x, high); ctx.lineTo(x, low); ctx.stroke();
      ctx.fillStyle = up ? "rgba(59,214,113,0.5)" : "rgba(255,92,122,0.5)";
      const y0 = Math.min(open, close); const h = Math.max(2, Math.abs(open-close));
      ctx.fillRect(x - width*0.25, y0, width*0.5, h);
    }
  }

  onMove(e){
    if (!this.scale) return;
    const rect = this.canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const { pad, minX, maxX, minY, maxY, w, h } = this.scale;
    if (x < pad.l || x > w-pad.r || y < pad.t || y > h-pad.b){ this.onLeave(); return; }

    const xVal = unmapX(x, pad, minX, maxX, w);
    const nearest = findNearest(this.series, this.hidden, xVal);
    if (!nearest) return;

    const cx = mapX(nearest.point.x, pad, minX, maxX, w);
    const cy = mapY(nearest.point.y, pad, minY, maxY, h);

    const overlay = this.overlay;
    overlay.innerHTML = "";
    const cross = document.createElement("div");
    cross.style.position = "absolute";
    cross.style.left = (cx-1) + "px";
    cross.style.top = pad.t + "px";
    cross.style.width = "2px";
    cross.style.height = (h-pad.t-pad.b) + "px";
    cross.style.background = "rgba(255,255,255,0.25)";
    overlay.appendChild(cross);

    const crossY = document.createElement("div");
    crossY.style.position = "absolute";
    crossY.style.left = pad.l + "px";
    crossY.style.top = (cy-1) + "px";
    crossY.style.width = (w-pad.l-pad.r) + "px";
    crossY.style.height = "2px";
    crossY.style.background = "rgba(255,255,255,0.25)";
    overlay.appendChild(crossY);

    this.tooltip.style.display = "block";
    this.tooltip.style.left = Math.min(cx+12, w-220) + "px";
    this.tooltip.style.top = Math.max(pad.t, cy-20) + "px";
    this.tooltip.innerHTML = `
      <div><b>${esc(nearest.series.key)}</b></div>
      <div>time: ${esc(formatTime(nearest.point.x))}</div>
      <div>value: ${esc(nearest.point.y.toFixed(2))}</div>
    `;
  }

  onLeave(){
    this.overlay.innerHTML = "";
    this.tooltip.style.display = "none";
  }

  onWheel(e){
    if (!this.scale) return;
    e.preventDefault();
    const rect = this.canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const { pad, minX, maxX, w } = this.scale;
    if (x < pad.l || x > w-pad.r) return;

    const xVal = unmapX(x, pad, minX, maxX, w);
    const zoomFactor = e.deltaY < 0 ? 0.9 : 1.1;
    const range = (this.zoom ? this.zoom.max - this.zoom.min : maxX - minX) * zoomFactor;
    const newMin = xVal - (xVal - (this.zoom ? this.zoom.min : minX)) * zoomFactor;
    const newMax = newMin + range;
    this.zoom = { min: newMin, max: newMax };
    this.render();
  }

  onDown(e){
    const rect = this.canvas.getBoundingClientRect();
    const x = e.clientX - rect.left;
    this.drag = { x0: x, x1: x };
  }

  onDrag(e){
    if (!this.drag) return;
    const rect = this.canvas.getBoundingClientRect();
    this.drag.x1 = e.clientX - rect.left;
    this.render();
  }

  onUp(e){
    if (!this.drag || !this.scale) return;
    const { pad, minX, maxX, w } = this.scale;
    const x0 = Math.min(this.drag.x0, this.drag.x1);
    const x1 = Math.max(this.drag.x0, this.drag.x1);
    this.drag = null;
    if (x1 - x0 < 10){ this.render(); return; }
    const min = unmapX(Math.max(x0, pad.l), pad, minX, maxX, w);
    const max = unmapX(Math.min(x1, w-pad.r), pad, minX, maxX, w);
    this.zoom = { min, max };
    this.render();
  }
}

function mapX(x, pad, minX, maxX, w){ return pad.l + (x - minX) / (maxX - minX) * (w - pad.l - pad.r); }
function mapY(y, pad, minY, maxY, h){ return pad.t + (maxY - y) / (maxY - minY) * (h - pad.t - pad.b); }
function unmapX(px, pad, minX, maxX, w){ return minX + (px - pad.l) / (w - pad.l - pad.r) * (maxX - minX); }

function findNearest(series, hidden, xVal){
  let best = null;
  for (const s of series){
    if (hidden.has(s.key)) continue;
    for (const p of s.points){
      const d = Math.abs(p.x - xVal);
      if (!best || d < best.d){ best = { d, series: s, point: p }; }
    }
  }
  return best;
}

function bucketOHLC(points, maxBuckets, minX, maxX){
  const buckets = [];
  if (!points.length) return buckets;
  const range = maxX - minX;
  const bucketSize = range / Math.max(1, maxBuckets);
  const map = new Map();
  for (const p of points){
    if (p.x < minX || p.x > maxX) continue;
    const idx = Math.floor((p.x - minX) / bucketSize);
    const key = idx;
    if (!map.has(key)) map.set(key, { x: p.x, open: p.y, close: p.y, high: p.y, low: p.y, lastX: p.x });
    const b = map.get(key);
    if (p.x < b.lastX){ b.open = p.y; b.lastX = p.x; }
    b.close = p.y;
    if (p.y > b.high) b.high = p.y;
    if (p.y < b.low) b.low = p.y;
  }
  for (const b of map.values()) buckets.push(b);
  buckets.sort((a,b)=>a.x-b.x);
  return buckets;
}

function renderLegend(root, series, chart){
  root.innerHTML = "";
  for (const s of series){
    const item = document.createElement("div");
    item.className = "legendItem";
    item.innerHTML = `<span class="legendSwatch" style="background:${s.color}"></span><span>${esc(s.key)}</span>`;
    item.addEventListener("click", ()=>{ item.classList.toggle("off"); chart.toggleSeries(s.key); });
    root.appendChild(item);
  }
}

function extractSeries(conf, rows){
  const records = expandRows(rows, false).map(x=>x.record);
  const seriesMap = new Map();
  for (const r of records){
    const xRaw = getPath(r, conf.x) ?? r.timestamp ?? r.occurred_at;
    const yRaw = getPath(r, conf.y);
    const x = coerceX(xRaw);
    const y = toNumber(yRaw);
    if (x == null || y == null) continue;
    const g = conf.g ? (getPath(r, conf.g) ?? "") : "";
    const key = conf.g ? `${conf.id}:${g}` : conf.id;
    if (!seriesMap.has(key)) seriesMap.set(key, []);
    seriesMap.get(key).push({ x, y });
  }
  const out = [];
  for (const [key, pts] of seriesMap){
    pts.sort((a,b)=>a.x-b.x || String(key).localeCompare(String(key)));
    out.push({ key, points: pts, color: palette(key) });
  }
  out.sort((a,b)=>a.key.localeCompare(b.key));
  return out;
}

function capSeries(series, cap){
  let total = 0; for (const s of series) total += s.points.length;
  if (total <= cap) return { series, total, truncated:false };
  const stride = Math.ceil(total / cap);
  const out = series.map(s=>({ ...s, points: s.points.filter((_,i)=>i%stride===0) }));
  return { series: out, total, truncated:true };
}

function coerceX(v){
  if (v == null) return null;
  if (typeof v === "number") return v;
  const s = String(v).trim();
  if (/^\d{4}-\d{2}-\d{2}T/.test(s)){
    const t = Date.parse(s); return isNaN(t) ? null : t;
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(s)){
    const t = Date.parse(s+"T00:00:00Z"); return isNaN(t) ? null : t;
  }
  if (/^\d{4}$/.test(s)){
    const t = Date.parse(s+"-01-01T00:00:00Z"); return isNaN(t) ? null : t;
  }
  const n = Number(s); return isFinite(n) ? n : null;
}

function formatTime(x){
  if (x > 1e12){
    const d = new Date(x);
    return d.toISOString();
  }
  return String(x);
}

function palette(key){
  const colors = ["#4ea1ff","#7b5cff","#ff5c7a","#3bd671","#ffcc66","#5ce1ff","#c27bff","#ffd166"];
  const h = hash(key);
  return colors[h % colors.length];
}

function hash(s){
  let h = 0; const str = String(s);
  for (let i=0;i<str.length;i++) h = ((h<<5)-h) + str.charCodeAt(i) | 0;
  return Math.abs(h);
}

function renderInspector(root, obj){
  const json = JSON.stringify(obj, null, 2);
  const big = json.length > 12000;
  root.innerHTML = `<div class="row" style="justify-content:space-between;"><span class="badge ${big?"warn":""}">${big?"large JSON (truncated)":"JSON"}</span>${big?`<button class="btn" id="expand">Expand Full</button>`:""}</div><div class="hr"></div><pre id="json">${esc(big?json.slice(0,12000)+"\n(truncated)":json)}</pre>`;
  if (big){ $("#expand").addEventListener("click", ()=>{ $("#json").textContent = json; toast("good","Expanded","Full JSON rendered."); $("#expand").remove(); }); }
}

function trunc(s,n){ const t = String(s||""); return t.length<=n ? t : t.slice(0,n)+""; }

function expandRows(rows, includeMeta){
  const out = [];
  for (const r of rows){
    let d = r.data;
    if (typeof d === "string"){ try{ d = JSON.parse(d); }catch{ d = null; } }
    const items = Array.isArray(d) ? d : (d && typeof d==="object" ? [d] : []);
    for (const rec of items){
      if (!rec || typeof rec!=="object") continue;
      const record = includeMeta ? { _meta:{ id:r.id, drone_id:r.drone_id, profile_id:r.profile_id, timestamp:r.timestamp }, ...rec } : rec;
      out.push({ record });
    }
  }
  return out;
}

function toNumber(x){
  if (x==null) return null;
  if (typeof x==="number" && Number.isFinite(x)) return x;
  const n = Number(String(x).replaceAll(",","").trim());
  return Number.isFinite(n) ? n : null;
}

function getPath(obj, path){
  const p = String(path||"").trim();
  if (!p) return undefined;
  const norm = p.replace(/\[(\d+)\]/g, ".$1");
  const parts = norm.split(".").filter(Boolean);
  let cur = obj;
  for (const part of parts){
    if (cur==null) return undefined;
    const isIndex = /^[0-9]+$/.test(part);
    if (isIndex && Array.isArray(cur)) cur = cur[Number(part)];
    else if (typeof cur==="object") cur = cur[part];
    else return undefined;
  }
  return cur;
}

function flatten(obj, prefix="", out={}){
  if (obj==null) return out;
  if (Array.isArray(obj)){ out[prefix||"value"]=JSON.stringify(obj); return out; }
  if (typeof obj!=="object"){ out[prefix||"value"]=String(obj); return out; }
  for (const k of Object.keys(obj)){
    const v = obj[k];
    const key = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v==="object" && !Array.isArray(v)) flatten(v,key,out);
    else if (Array.isArray(v)) out[key]=JSON.stringify(v);
    else out[key]=v==null?"":String(v);
  }
  return out;
}

function toCsv(rows){
  const flat = rows.map(r=>flatten(r));
  const keys = Array.from(new Set(flat.flatMap(o=>Object.keys(o)))).sort((a,b)=>a.localeCompare(b));
  const escCsv = (s)=>{ const v = String(s??""); if (/[,"\n]/.test(v)) return `"${v.replaceAll('"','""')}"`; return v; };
  const head = keys.map(escCsv).join(",");
  const lines = flat.map(o=>keys.map(k=>escCsv(o[k]??"")).join(","));
  return [head, ...lines].join("\n") + "\n";
}

function downloadText(name, text, mime){
  const blob = new Blob([text], { type: mime || "text/plain;charset=utf-8" });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  URL.revokeObjectURL(a.href);
}

function joinTwo(A,B, pathA, pathB){
  const mapA = new Map();
  for (const a of A){
    const k = getPath(a, pathA); if (k==null) continue; const ks = String(k);
    if (!mapA.has(ks)) mapA.set(ks, []); mapA.get(ks).push(a);
  }
  const joined = [];
  for (const b of B){
    const k = getPath(b, pathB); if (k==null) continue; const ks = String(k);
    const as = mapA.get(ks); if (!as) continue;
    joined.push({ key: ks, a: as[0], b });
  }
  joined.sort((x,y)=>String(x.key).localeCompare(String(y.key)));
  return joined;
}

function pearson(joined, pathA, pathB){
  const xs=[], ys=[];
  for (const j of joined){
    const a = toNumber(getPath(j.a, pathA));
    const b = toNumber(getPath(j.b, pathB));
    if (a==null || b==null) continue;
    xs.push(a); ys.push(b);
  }
  const n = xs.length;
  if (n < 2) return { r: 0, n };
  const mean = arr => arr.reduce((s,v)=>s+v,0)/arr.length;
  const mx = mean(xs), my = mean(ys);
  let num=0, dx=0, dy=0;
  for (let i=0;i<n;i++){ const vx = xs[i]-mx; const vy = ys[i]-my; num += vx*vy; dx += vx*vx; dy += vy*vy; }
  const den = Math.sqrt(dx*dy);
  const r = den===0 ? 0 : (num/den);
  return { r, n };
}
