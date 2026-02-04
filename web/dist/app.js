
/* Chartly Control Plane UI  vanilla no-build SPA
   - Fixes common UI bugs: routing, relative paths, error handling, empty states, request visibility
   - Uses same-origin /api/* calls only (no hardcoded localhost)
*/

const $ = (sel, root=document) => root.querySelector(sel);
const $$ = (sel, root=document) => Array.from(root.querySelectorAll(sel));

const STORAGE = {
  apiKey: "chartly_api_key",
  limit: "chartly_default_limit",
};

const state = {
  apiKey: sessionStorage.getItem(STORAGE.apiKey) || "",
  limit: Number(sessionStorage.getItem(STORAGE.limit) || "100"),
  services: { registry: "unknown", aggregator: "unknown", coordinator: "unknown", reporter: "unknown" },
  counts: { profiles: "", drones: "", results: "" },
  lastReq: { id: "", info: "No requests yet" },
  cache: { profiles: [], drones: [], summary: null },
};

const ROUTES = [
  { path: "/", label: "Dashboard", render: pageDashboard },
  { path: "/profiles", label: "Profiles", render: pageProfiles },
  { path: "/drones", label: "Drones", render: pageDrones },
  { path: "/results", label: "Results", render: pageResults },
  { path: "/charts", label: "Charts", render: pageCharts },
  { path: "/runs", label: "Runs", render: pageRuns },
  { path: "/correlate", label: "Correlate", render: pageCorrelate },
  { path: "/reports", label: "Reports", render: pageReports },
  { path: "/settings", label: "Settings", render: pageSettings },
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
  $$("#nav a").forEach(a=>{
    a.classList.toggle("active", a.getAttribute("href") === path);
  });
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
  try {
    await r.render(page, { force, soft });
  } catch (err){
    toast("bad","UI Error", msg(err));
    page.innerHTML = errorCard("Render failed", err);
  }
}

/* ---------------- API Layer ---------------- */

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
    try {
      const res = await fetchTimeout(url, { ...opts, headers }, timeoutMs);
      const text = await res.text();
      state.lastReq = { id, info: `${opts.method || "GET"} ${url} -> ${res.status}` };
      renderLastReq();

      let data = null;
      if (text && text.trim().length){
        try { data = JSON.parse(text); } catch { data = { raw:text }; }
      }
      if (!res.ok){
        const e = new Error((data && data.message) ? data.message : `HTTP ${res.status}`);
        e.status = res.status; e.data = data;
        throw e;
      }
      return data;
    } catch (e){
      last = e;
      const st = (e && typeof e.status === "number") ? e.status : 0;
      const retryable = st === 0 || st >= 500 || st === 429;
      if (a < retries && retryable){
        await sleep([800, 1500, 3000][a] || 3000);
        continue;
      }
      throw last;
    }
  }
  throw last || new Error("request_failed");
}

/* --------------- UI primitives --------------- */

function esc(s){
  return String(s ?? "")
    .replaceAll("&","&amp;").replaceAll("<","&lt;")
    .replaceAll(">","&gt;").replaceAll('"',"&quot;")
    .replaceAll("'","&#39;");
}

function msg(e){
  return (e && e.message) ? String(e.message) : String(e);
}

function toast(kind, title, text){
  const host = $("#toasts");
  const el = document.createElement("div");
  el.className = `toast ${kind}`;
  el.innerHTML = `<div class="t">${esc(title)}</div><div class="small muted">${esc(text)}</div>`;
  host.appendChild(el);
  setTimeout(()=>{
    el.style.opacity="0";
    el.style.transform="translateY(4px)";
    setTimeout(()=>el.remove(), 220);
  }, 3500);
}

function skeleton(title){
  return `
    <div class="card">
      <div class="h1">${esc(title)}</div>
      <p class="hint">Loading</p>
    </div>
  `;
}

function errorCard(title, err){
  const detail = esc(String(err && err.stack ? err.stack : err));
  return `
    <div class="card">
      <div class="h1">${esc(title)}</div>
      <p class="hint">This page hit an error. Try Refresh. If it persists, check gateway logs.</p>
      <pre>${detail}</pre>
    </div>
  `;
}

function badgeClass(v){
  const s = String(v||"").toLowerCase();
  if (s==="up"||s==="healthy"||s==="ok") return "good";
  if (s==="down"||s==="unhealthy"||s==="error") return "bad";
  if (s==="unknown"||s==="") return "";
  return "warn";
}

function normalizeServiceStatus(v){
  if (v == null) return "unknown";
  if (typeof v === "string") return v;
  if (typeof v === "object") {
    if (typeof v.status === "string") return v.status;
    if (typeof v.Status === "string") return v.Status;
  }
  return "unknown";
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

/* --------------- Sidebar refresh --------------- */

async function refreshSidebar({ soft=true } = {}){
  // Gateway status
  try {
    const st = await apiJson("/api/status", {}, { retries: soft ? 0 : 1, timeoutMs: 8000 });
    const gw = st && st.status ? st.status : "unknown";
    $("#gwBadge").textContent = gw;
    $("#gwBadge").className = `badge ${badgeClass(gw)}`;

    const sv = (st && st.services) ? st.services : {};
    state.services.registry = normalizeServiceStatus(sv.registry);
    state.services.aggregator = normalizeServiceStatus(sv.aggregator);
    state.services.coordinator = normalizeServiceStatus(sv.coordinator);
    state.services.reporter = normalizeServiceStatus(sv.reporter);

    setSvc("#svcRegistry", state.services.registry);
    setSvc("#svcAggregator", state.services.aggregator);
    setSvc("#svcCoordinator", state.services.coordinator);
    setSvc("#svcReporter", state.services.reporter);

  } catch {
    // fallback /health
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
      if (!soft) toast("bad","Gateway Down","Could not reach /api/status or /health");
      return;
    }
  }

  // counts best-effort
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
/* ------------------- Pages ------------------- */

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
      </div>

      <div class="hr"></div>

      <div class="grid3">
        <div class="panel">
          <div class="panelTitle">Profiles</div>
          <div class="h1">${esc(state.counts.profiles)}</div>
          <div class="small muted">Definitions drones execute.</div>
          <div class="row" style="margin-top:10px;">
            <button class="btn" id="goProfiles">Open</button>
          </div>
        </div>

        <div class="panel">
          <div class="panelTitle">Drones</div>
          <div class="h1">${esc(state.counts.drones)}</div>
          <div class="small muted">Active drones seen recently.</div>
          <div class="row" style="margin-top:10px;">
            <button class="btn" id="goDrones">Open</button>
          </div>
        </div>

        <div class="panel">
          <div class="panelTitle">Results</div>
          <div class="h1">${esc(state.counts.results)}</div>
          <div class="small muted">Stored outputs (summary).</div>
          <div class="row" style="margin-top:10px;">
            <button class="btn" id="goResults">Open</button>
          </div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="h1">Overlapped Reporting</div>
      <p class="hint">Join two datasets by a shared key (JSON path) and compute correlation when possible.</p>
      <div class="row">
        <button class="btn" id="goCorr">Open Correlate</button>
        <a class="btn ghost" href="/runs" data-link>Runs</a>
      </div>
      <div class="hr"></div>
      <div class="small muted">
        ${sum ? esc(JSON.stringify(sum, null, 2)).slice(0, 900) : "Summary not available yet."}
      </div>
    </div>
  `;

  $("#goProfiles").addEventListener("click", ()=>navigate("/profiles"));
  $("#goDrones").addEventListener("click", ()=>navigate("/drones"));
  $("#goResults").addEventListener("click", ()=>navigate("/results"));
  $("#goCorr").addEventListener("click", ()=>navigate("/correlate"));
}

async function pageProfiles(root){
  if (!state.cache.profiles.length) await refreshSidebar({ soft:false });
  const profiles = state.cache.profiles;

  root.innerHTML = `
    <div class="card">
      <div class="h1">Profiles</div>
      <p class="hint">View YAML. Create requires API key in Settings.</p>
      <div class="row">
        <button class="btn" id="reload">Reload</button>
        <button class="btn" id="openSettings">Set API Key</button>
      </div>
      <div class="hr"></div>
      ${profiles.length === 0 ? `<div class="small muted">No profiles found. Add YAML under profiles/* or create via API.</div>` : `
      <div class="tableWrap">
        <table class="table">
          <thead><tr><th>id</th><th>name</th><th>version</th><th>status</th><th>actions</th></tr></thead>
          <tbody>
            ${profiles.map(p=>`
              <tr>
                <td class="mono">${esc(p.id||"")}</td>
                <td>${esc(p.name||"")}</td>
                <td class="mono">${esc(p.version||"")}</td>
                <td><span class="badge" data-status="${esc(p.id||"")}">unknown</span></td>
                <td><button class="btn" data-view="${esc(p.id||"")}">View</button></td>
              </tr>
            `).join("")}
          </tbody>
        </table>
      </div>`}
    </div>

    <div class="card">
      <div class="h1">View / Create</div>
      <p class="hint">Inspect YAML + optionally create a new profile.</p>

      <div class="grid2">
        <div class="panel">
          <div class="panelTitle">Selected Profile</div>
          <div id="viewer" class="small muted">Select a profile to view its YAML.</div>
        </div>

        <div class="panel">
          <div class="panelTitle">Create (POST /api/profiles)</div>
          <div class="small muted">No secrets in YAML. API key is session-only.</div>
          <div style="height:10px;"></div>
          <textarea id="yaml" class="input" spellcheck="false" placeholder="id: my-profile&#10;name: Example&#10;version: 1.0.0&#10;source: ..."></textarea>
          <div class="row" style="margin-top:10px;">
            <button class="btn" id="extract">Extract</button>
            <button class="btn" id="create">Create</button>
          </div>
          <div class="small muted" style="margin-top:10px;">
            Extracted: <span class="mono" id="meta"></span>
          </div>
        </div>
      </div>
    </div>
  `;

  $("#reload").addEventListener("click", async ()=>{
    await refreshSidebar({ soft:false });
    toast("good","Reloaded","Profiles refreshed");
    route("/profiles", { force:true, soft:true });
  });
  $("#openSettings").addEventListener("click", ()=>navigate("/settings"));

  root.addEventListener("click", async (e)=>{
    const b = e.target.closest("button[data-view]");
    if (!b) return;
    await viewProfile(b.getAttribute("data-view"));
  });

  $("#extract").addEventListener("click", ()=>{
    const m = extractMeta($("#yaml").value);
    $("#meta").textContent = `id=${m.id||"?"} name=${m.name||"?"} version=${m.version||"?"}`;
    toast("good","Extracted", $("#meta").textContent);
  });

  $("#create").addEventListener("click", async ()=>{
    if (!state.apiKey){
      toast("bad","Missing API Key","Go to Settings and set X-API-Key (session-only).");
      navigate("/settings"); return;
    }
    const yaml = $("#yaml").value || "";
    const m = extractMeta(yaml);
    if (!yaml.trim() || !m.id){
      toast("warn","Invalid YAML","YAML must include an id: field.");
      return;
    }
    try{
      await apiJson("/api/profiles", {
        method:"POST",
        headers:{ "Content-Type":"application/json", "X-API-Key": state.apiKey },
        body: JSON.stringify({ id:m.id, name:m.name||m.id, version:m.version||"0.0.0", content: yaml })
      });
      toast("good","Created", m.id);
      $("#yaml").value = "";
      $("#meta").textContent = "";
      await refreshSidebar({ soft:false });
      route("/profiles", { force:true, soft:true });
    }catch(e){
      toast("bad","Create failed", msg(e));
    }
  });

  // Load statuses best-effort if endpoint exists
  loadProfileStatuses().catch(()=>{});
}

async function loadProfileStatuses(){
  const badges = $$("span[data-status]");
  if (badges.length === 0) return;
  // try one call to see if endpoint exists
  for (const el of badges){
    const id = el.getAttribute("data-status");
    try{
      const st = await apiJson(`/api/profiles/${encodeURIComponent(id)}/status`, {}, { retries:0, timeoutMs:6000 });
      const last = st && st.last_run ? st.last_run : null;
      if (!last){
        el.textContent = "no runs";
        continue;
      }
      el.textContent = last.status || "unknown";
      el.className = `badge ${badgeClass(last.status)}`;
      el.title = last.error ? String(last.error).slice(0,200) : "";
    }catch{
      // endpoint may not exist; keep unknown and stop hammering
      el.textContent = "planned";
      el.className = "badge warn";
    }
  }
}

async function viewProfile(id){
  const v = $("#viewer");
  v.innerHTML = `<div class="small muted">Loading <span class="mono">${esc(id)}</span></div>`;
  try{
    const p = await apiJson(`/api/profiles/${encodeURIComponent(id)}`, {}, { retries:1, timeoutMs:12000 });
    const yaml = (p && p.content) ? String(p.content) : "";
    v.innerHTML = `
      <div class="row" style="justify-content:space-between; align-items:flex-start;">
        <div>
          <div class="panelTitle">${esc(p.name||p.id||id)}</div>
          <div class="small muted">id=<span class="mono">${esc(p.id||id)}</span> version=<span class="mono">${esc(p.version||"")}</span></div>
        </div>
        <div class="row">
          <button class="btn" id="copyId">Copy ID</button>
          <button class="btn" id="dlYaml">Download</button>
        </div>
      </div>
      <div class="hr"></div>
      <pre>${esc(yaml)}</pre>
    `;
    $("#copyId").addEventListener("click", async ()=>{
      try{ await navigator.clipboard.writeText(String(p.id||id)); toast("good","Copied","Profile id copied"); }
      catch{ toast("warn","Copy failed","Clipboard unavailable"); }
    });
    $("#dlYaml").addEventListener("click", ()=>{
      downloadText(`${String(p.id||id)}.yaml`, yaml, "text/yaml;charset=utf-8");
    });
  }catch(e){
    v.innerHTML = `<pre>${esc(msg(e))}</pre>`;
  }
}

function extractMeta(yaml){
  const out = { id:"", name:"", version:"" };
  const lines = String(yaml||"").split(/\r?\n/);
  for (const line of lines){
    const m = line.match(/^\s*(id|name|version)\s*:\s*(.+?)\s*$/i);
    if (!m) continue;
    const k = m[1].toLowerCase();
    const v = String(m[2]).replace(/^["']|["']$/g,"").trim();
    if (k==="id" && !out.id) out.id = v;
    if (k==="name" && !out.name) out.name = v;
    if (k==="version" && !out.version) out.version = v;
    if (out.id && out.name && out.version) break;
  }
  return out;
}

async function pageDrones(root){
  await refreshSidebar({ soft:false });

  let stats = null;
  try{ stats = await apiJson("/api/drones/stats", {}, { retries:1, timeoutMs:8000 }); }catch{}

  const drones = state.cache.drones || [];
  root.innerHTML = `
    <div class="card">
      <div class="h1">Drones</div>
      <p class="hint">Active drones are those with recent heartbeats.</p>
      <div class="row">
        <span class="badge">listed=${esc(drones.length)}</span>
        <span class="badge">${stats ? `total=${stats.total} active=${stats.active} offline=${stats.offline}` : "stats: planned"}</span>
        <button class="btn" id="reload">Reload</button>
      </div>
      <div class="hr"></div>
      ${drones.length === 0 ? `<div class="small muted">No active drones. Start one with scripts/drone-up.*</div>` : `
      <div class="tableWrap">
        <table class="table">
          <thead><tr><th>id</th><th>status</th><th>last_heartbeat</th><th>registered_at</th><th>profiles</th></tr></thead>
          <tbody>
            ${drones.map(d=>`
              <tr>
                <td class="mono">${esc(d.id||"")}</td>
                <td><span class="badge ${badgeClass(d.status)}">${esc(d.status||"")}</span></td>
                <td class="mono">${esc(d.last_heartbeat||"")}</td>
                <td class="mono">${esc(d.registered_at||"")}</td>
                <td class="mono">${esc(Array.isArray(d.assigned_profiles)?d.assigned_profiles.length:0)}</td>
              </tr>
            `).join("")}
          </tbody>
        </table>
      </div>`}
    </div>
  `;
  $("#reload").addEventListener("click", async ()=>{
    await refreshSidebar({ soft:false });
    toast("good","Reloaded",`drones=${state.cache.drones.length}`);
    route("/drones",{ force:true, soft:true });
  });
}

async function pageResults(root){
  await refreshSidebar({ soft:false });

  const profiles = state.cache.profiles || [];
  root.innerHTML = `
    <div class="card">
      <div class="h1">Results</div>
      <p class="hint">Query stored results and inspect rows. Export CSV from expanded record data.</p>

      <div class="grid3">
        <div>
          <div class="small muted">drone_id (optional)</div>
          <input id="qDrone" class="input" placeholder="drone-123" />
        </div>
        <div>
          <div class="small muted">profile_id (optional)</div>
          <select id="qProfile" class="input">
            <option value="">(any)</option>
            ${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}
          </select>
        </div>
        <div>
          <div class="small muted">limit</div>
          <input id="qLimit" class="input" value="${esc(state.limit)}" />
        </div>
      </div>

      <div class="row" style="margin-top:12px;">
        <button class="btn" id="btnQuery">Run Query</button>
        <button class="btn" id="btnSummary">Load Summary</button>
        <button class="btn" id="btnExport" disabled>Export CSV</button>
      </div>
    </div>

    <div class="card">
      <div class="h1">Summary</div>
      <p class="hint">/api/results/summary</p>
      <div id="sumBox" class="panel"><div class="small muted">Not loaded.</div></div>
    </div>

    <div class="card">
      <div class="h1">Query Output</div>
      <p class="hint">Click Inspect to view a row. Large JSON is truncated with expand option.</p>
      <div id="outBox" class="panel"><div class="small muted">Run a query above.</div></div>
    </div>
  `;

  let lastRows = [];
  $("#btnSummary").addEventListener("click", async ()=>{
    const box = $("#sumBox");
    box.innerHTML = `<div class="small muted">Loading</div>`;
    try{
      const sum = await apiJson("/api/results/summary", {}, { retries:1, timeoutMs:12000 });
      box.innerHTML = `<pre>${esc(JSON.stringify(sum, null, 2))}</pre>`;
    }catch(e){
      box.innerHTML = `<pre>${esc(msg(e))}</pre>`;
    }
  });

  $("#btnQuery").addEventListener("click", async ()=>{
    const drone = $("#qDrone").value.trim();
    const profile = $("#qProfile").value.trim();
    const limit = parseInt($("#qLimit").value.trim(),10) || state.limit || 100;

    const qs = new URLSearchParams();
    if (drone) qs.set("drone_id", drone);
    if (profile) qs.set("profile_id", profile);
    qs.set("limit", String(limit));

    const box = $("#outBox");
    box.innerHTML = `<div class="small muted">Querying</div>`;
    try{
      const rows = await apiJson(`/api/results?${qs.toString()}`, {}, { retries:1, timeoutMs:20000 });
      lastRows = Array.isArray(rows) ? rows : [];
      $("#btnExport").disabled = lastRows.length === 0;

      box.innerHTML = `
        <div class="small muted">rows=${esc(lastRows.length)}</div>
        <div class="hr"></div>
        ${lastRows.length===0 ? `<div class="small muted">No results. Start drones and wait for processing.</div>` : `
        <div class="tableWrap">
          <table class="table">
            <thead><tr><th>timestamp</th><th>drone_id</th><th>profile_id</th><th>inspect</th></tr></thead>
            <tbody>
              ${lastRows.map((r,i)=>`
                <tr>
                  <td class="mono">${esc(r.timestamp||"")}</td>
                  <td class="mono">${esc(r.drone_id||"")}</td>
                  <td class="mono">${esc(r.profile_id||"")}</td>
                  <td><button class="btn" data-inspect="${i}">Inspect</button></td>
                </tr>
              `).join("")}
            </tbody>
          </table>
        </div>`}
        <div class="hr"></div>
        <div id="inspectBox" class="panel"><div class="small muted">Click Inspect on a row.</div></div>
      `;

      box.addEventListener("click", (e)=>{
        const b = e.target.closest("button[data-inspect]");
        if (!b) return;
        const i = parseInt(b.getAttribute("data-inspect"),10);
        const row = lastRows[i];
        renderInspector($("#inspectBox"), row);
      });

      toast("good","Query complete",`rows=${lastRows.length}`);
    }catch(e){
      box.innerHTML = `<pre>${esc(msg(e))}</pre>`;
      toast("bad","Query failed", msg(e));
    }
  });

  $("#btnExport").addEventListener("click", ()=>{
    if (!lastRows.length) return;
    const records = expandRows(lastRows, true).map(x=>x.record);
    const csv = toCsv(records);
    downloadText("results.csv", csv, "text/csv;charset=utf-8");
    toast("good","Exported","results.csv downloaded");
  });
}

async function pageCharts(root){
  await refreshSidebar({ soft:false });
  const profiles = state.cache.profiles || [];

  root.innerHTML = `
    <div class="card">
      <div class="h1">Charts</div>
      <p class="hint">Stock-style lines with optional prediction. Select 1 or many profiles and a numeric path.</p>

      <div class="grid3">
        <div>
          <div class="small muted">Profiles (multi-select)</div>
          <select id="chartProfiles" class="input" multiple>
            ${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}
          </select>
        </div>
        <div>
          <div class="small muted">X path (date/number)</div>
          <input id="chartX" class="input" value="dims.time.date" />
          <div class="small muted" style="margin-top:8px;">Examples: dims.time.date, dims.time.year</div>
        </div>
        <div>
          <div class="small muted">Y path (numeric)</div>
          <input id="chartY" class="input" value="measures.population.total" />
          <div class="small muted" style="margin-top:8px;">Example: measures.employment.rate</div>
        </div>
      </div>

      <div class="grid3" style="margin-top:12px;">
        <div>
          <div class="small muted">limit per profile</div>
          <input id="chartLimit" class="input" value="${esc(state.limit)}" />
        </div>
        <div>
          <div class="small muted">prediction points</div>
          <input id="chartPred" class="input" value="10" />
        </div>
        <div>
          <div class="small muted">actions</div>
          <div class="row">
            <button class="btn" id="chartRender">Render</button>
            <label class="small muted"><input type="checkbox" id="chartPredict" checked /> Predict</label>
          </div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="h1">Chart</div>
      <p class="hint">Lines are sorted by time. Prediction uses simple linear regression over the most recent points.</p>
      <div class="chartWrap">
        <canvas id="chartCanvas" class="chartCanvas" width="1200" height="400"></canvas>
      </div>
      <div id="chartLegend" class="legend"></div>
      <div id="chartMsg" class="small muted" style="margin-top:8px;">Select profiles and click Render.</div>
    </div>
  `;

  $("#chartRender").addEventListener("click", async ()=>{
    const selected = Array.from($("#chartProfiles").selectedOptions).map(o=>o.value);
    const xPath = $("#chartX").value.trim();
    const yPath = $("#chartY").value.trim();
    const limit = parseInt($("#chartLimit").value.trim(),10) || state.limit || 100;
    const predict = $("#chartPredict").checked;
    const predPts = Math.max(1, Math.min(50, parseInt($("#chartPred").value.trim(),10) || 10));

    if (!selected.length) {
      toast("warn","No profiles","Select at least one profile.");
      return;
    }
    if (!xPath || !yPath) {
      toast("warn","Missing paths","Provide both X and Y paths.");
      return;
    }

    $("#chartMsg").textContent = "Fetching data...";
    try{
      const series = [];
      for (const pid of selected){
        const rows = await apiJson(`/api/results?profile_id=${encodeURIComponent(pid)}&limit=${encodeURIComponent(String(limit))}`, {}, { retries:1, timeoutMs:20000 });
        const recs = expandRows(Array.isArray(rows)?rows:[], false).map(x=>x.record);
        const pts = recs.map(r=>{
          const x = coerceX(getPath(r, xPath));
          const y = toNumber(getPath(r, yPath));
          if (x == null || y == null) return null;
          return { x, y };
        }).filter(Boolean).sort((a,b)=>a.x-b.x);
        series.push({ id: pid, points: pts });
      }

      const preds = predict ? series.map(s=>({ id: s.id, points: predictSeries(s.points, predPts) })) : [];
      drawChart($("#chartCanvas"), series, preds);
      renderLegend($("#chartLegend"), series, preds);
      $("#chartMsg").textContent = `series=${series.length} (points vary per profile)`;
      toast("good","Chart rendered", `profiles=${series.length}`);
    }catch(e){
      $("#chartMsg").textContent = "Failed to render chart.";
      toast("bad","Chart failed", msg(e));
    }
  });
}

async function pageRuns(root){
  root.innerHTML = `
    <div class="card">
      <div class="h1">Runs</div>
      <p class="hint">If your aggregator exposes /api/runs and /api/runs/{id}, youll see run history here.</p>
      <div class="row">
        <button class="btn" id="reload">Reload</button>
        <input id="limit" class="input" style="max-width:160px;" value="${esc(state.limit)}" />
      </div>
      <div class="hr"></div>
      <div id="runsBox" class="panel"><div class="small muted">Press Reload.</div></div>
    </div>
  `;

  $("#reload").addEventListener("click", async ()=>{
    const lim = parseInt($("#limit").value.trim(),10) || state.limit || 100;
    const box = $("#runsBox");
    box.innerHTML = `<div class="small muted">Loading</div>`;
    try{
      const runs = await apiJson(`/api/runs?limit=${encodeURIComponent(String(lim))}`, {}, { retries:1, timeoutMs:12000 });
      const arr = Array.isArray(runs) ? runs : [];
      arr.sort((a,b)=>String(b.started_at||"").localeCompare(String(a.started_at||"")) || String(a.run_id||"").localeCompare(String(b.run_id||"")));
      box.innerHTML = arr.length===0 ? `<div class="small muted">No runs returned. Endpoint may be planned or no drones have posted runs.</div>` : `
        <div class="tableWrap">
          <table class="table">
            <thead><tr><th>started_at</th><th>run_id</th><th>profile_id</th><th>drone_id</th><th>status</th><th>rows</th><th>inspect</th></tr></thead>
            <tbody>
              ${arr.map((r,i)=>`
                <tr>
                  <td class="mono">${esc(r.started_at||"")}</td>
                  <td class="mono">${esc(r.run_id||"")}</td>
                  <td class="mono">${esc(r.profile_id||"")}</td>
                  <td class="mono">${esc(r.drone_id||"")}</td>
                  <td><span class="badge ${badgeClass(r.status)}">${esc(r.status||"")}</span></td>
                  <td class="mono">${esc(r.rows_out ?? "")}</td>
                  <td><button class="btn" data-run="${esc(r.run_id||"")}">Inspect</button></td>
                </tr>
              `).join("")}
            </tbody>
          </table>
        </div>
        <div class="hr"></div>
        <div id="runInspect" class="panel"><div class="small muted">Click Inspect.</div></div>
      `;

      box.addEventListener("click", async (e)=>{
        const b = e.target.closest("button[data-run]");
        if (!b) return;
        const id = b.getAttribute("data-run");
        const t = $("#runInspect");
        t.innerHTML = `<div class="small muted">Loading ${esc(id)}</div>`;
        try{
          const one = await apiJson(`/api/runs/${encodeURIComponent(id)}`, {}, { retries:0, timeoutMs:8000 });
          renderInspector(t, one);
        }catch(err){
          // if /runs/{id} missing, fall back to simple message
          renderInspector(t, { note:"run detail endpoint may be planned", run_id:id });
        }
      });

      toast("good","Runs loaded",`count=${arr.length}`);
    }catch(e){
      box.innerHTML = `<div class="small muted">/api/runs not available (planned) or error:</div><pre>${esc(msg(e))}</pre>`;
      toast("warn","Runs planned", "Endpoint not available or returned error.");
    }
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
        <div class="panel">
          <div class="panelTitle">Dataset A</div>
          <div class="small muted">profile_id</div>
          <select id="aId" class="input">${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select>
          <div style="height:10px;"></div>
          <div class="small muted">join key path (A)</div>
          <input id="aJoin" class="input" value="location.state_code" />
          <div style="height:10px;"></div>
          <div class="small muted">numeric path (A) optional</div>
          <input id="aNum" class="input" placeholder="population.total" />
        </div>

        <div class="panel">
          <div class="panelTitle">Dataset B</div>
          <div class="small muted">profile_id</div>
          <select id="bId" class="input">${profiles.map(p=>`<option value="${esc(p.id)}">${esc(p.id)}  ${esc(p.name||"")}</option>`).join("")}</select>
          <div style="height:10px;"></div>
          <div class="small muted">join key path (B)</div>
          <input id="bJoin" class="input" value="location.state_code" />
          <div style="height:10px;"></div>
          <div class="small muted">numeric path (B) optional</div>
          <input id="bNum" class="input" placeholder="employment.rate" />
        </div>
      </div>

      <div class="grid3" style="margin-top:12px;">
        <div>
          <div class="small muted">limit per profile</div>
          <input id="lim" class="input" value="${esc(state.limit)}" />
        </div>
        <div>
          <div class="small muted">preview rows (max 500)</div>
          <input id="preview" class="input" value="200" />
        </div>
        <div>
          <div class="small muted">actions</div>
          <div class="row">
            <button class="btn" id="go">Join + Analyze</button>
            <button class="btn" id="exp" disabled>Export CSV</button>
          </div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="h1">Output</div>
      <p class="hint">Preview + correlation (if both numeric paths provided).</p>
      <div id="out" class="panel"><div class="small muted">Run Join + Analyze.</div></div>
    </div>
  `;

  if ($("#bId").options.length > 1) $("#bId").selectedIndex = 1;

  let joined = [];

  $("#go").addEventListener("click", async ()=>{
    const aId = $("#aId").value.trim();
    const bId = $("#bId").value.trim();
    const aJoin = $("#aJoin").value.trim();
    const bJoin = $("#bJoin").value.trim();
    const aNum = $("#aNum").value.trim();
    const bNum = $("#bNum").value.trim();
    const lim = parseInt($("#lim").value.trim(),10) || state.limit || 100;
    const prev = Math.max(1, Math.min(500, parseInt($("#preview").value.trim(),10) || 200));

    const out = $("#out");
    out.innerHTML = `<div class="small muted">Fetching results</div>`;
    $("#exp").disabled = true;
    joined = [];

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
        ${corr ? `<div class="row">
          <span class="badge">pearson_r: <span class="mono">${esc(corr.r.toFixed(6))}</span></span>
          <span class="badge">n: <span class="mono">${esc(corr.n)}</span></span>
          <span class="badge">fields: <span class="mono">${esc(aNum)}</span> vs <span class="mono">${esc(bNum)}</span></span>
        </div>` : `<div class="small muted">Provide numeric paths for correlation (optional).</div>`}
        <div class="hr"></div>
        ${joined.length===0 ? `<div class="small muted">No join matches. Verify join paths and that both datasets share keys.</div>` : `
        <div class="tableWrap">
          <table class="table">
            <thead><tr><th>join_key</th><th>A (truncated)</th><th>B (truncated)</th></tr></thead>
            <tbody>
              ${joined.slice(0,prev).map(j=>`
                <tr>
                  <td class="mono">${esc(j.key)}</td>
                  <td class="mono">${esc(trunc(JSON.stringify(j.a),220))}</td>
                  <td class="mono">${esc(trunc(JSON.stringify(j.b),220))}</td>
                </tr>
              `).join("")}
            </tbody>
          </table>
        </div>`}
      `;

      $("#exp").disabled = joined.length===0;
      toast("good","Join complete",`joined=${joined.length}`);
    }catch(e){
      out.innerHTML = `<pre>${esc(msg(e))}</pre>`;
      toast("bad","Join failed", msg(e));
    }
  });

  $("#exp").addEventListener("click", ()=>{
    if (!joined.length) return;
    const rows = joined.map(j=>({ join_key:j.key, a:j.a, b:j.b }));
    const csv = toCsv(rows);
    downloadText("joined.csv", csv, "text/csv;charset=utf-8");
    toast("good","Exported","joined.csv downloaded");
  });
}

async function pageReports(root){
  root.innerHTML = `
    <div class="card">
      <div class="h1">Reports</div>
      <p class="hint">Server-side reports (if /api/reports is enabled). This UI will show a clear planned notice if not implemented.</p>
      <div class="row">
        <button class="btn" id="pingReports">Ping /api/reports</button>
      </div>
      <div class="hr"></div>
      <div id="reportsBox" class="panel"><div class="small muted">Press Ping.</div></div>
    </div>
  `;

  $("#pingReports").addEventListener("click", async ()=>{
    const box = $("#reportsBox");
    box.innerHTML = `<div class="small muted">Checking...</div>`;
    try{
      const res = await apiJson("/api/reports", { method:"POST", headers:{ "Content-Type":"application/json" }, body: JSON.stringify({ join:[], inputs:[], window:{limit:1}, output:{type:"table"} }) }, { retries:0, timeoutMs:8000 });
      box.innerHTML = `<pre>${esc(JSON.stringify(res, null, 2))}</pre>`;
    }catch(e){
      box.innerHTML = `<div class="small muted">/api/reports is not available or returned error:</div><pre>${esc(msg(e))}</pre>`;
      toast("warn","Reports planned","Endpoint not available or returned error.");
    }
  });
}

async function pageSettings(root){
  root.innerHTML = `
    <div class="card">
      <div class="h1">Settings</div>
      <p class="hint">API key is stored only for this browser tab/session.</p>

      <div class="grid2">
        <div>
          <div class="small muted">X-API-Key (session only)</div>
          <input id="key" class="input" placeholder="(not persisted)" value="${esc(state.apiKey)}" />
          <div class="small muted" style="margin-top:8px;">
            Used for <code>POST /api/profiles</code>.
          </div>
        </div>

        <div>
          <div class="small muted">Default limit</div>
          <input id="lim" class="input" value="${esc(state.limit)}" />
          <div class="small muted" style="margin-top:8px;">
            Used by Results/Correlate pages.
          </div>
        </div>
      </div>

      <div class="row" style="margin-top:12px;">
        <button class="btn" id="save">Save</button>
        <button class="btn" id="clear">Clear Session</button>
      </div>

      <div class="hr"></div>
      <div class="small muted">
        <div><b>Noticeable Key Slot:</b> paste your API key above, then click Save.</div>
        <div>It will never be written to disk by this UI.</div>
      </div>
    </div>
  `;

  $("#save").addEventListener("click", ()=>{
    const k = $("#key").value.trim();
    const lim = parseInt($("#lim").value.trim(),10) || 100;
    state.apiKey = k;
    state.limit = lim;
    sessionStorage.setItem(STORAGE.apiKey, k);
    sessionStorage.setItem(STORAGE.limit, String(lim));
    toast("good","Saved","Session settings updated.");
    navigate("/");
  });

  $("#clear").addEventListener("click", ()=>{
    sessionStorage.removeItem(STORAGE.apiKey);
    sessionStorage.removeItem(STORAGE.limit);
    state.apiKey = "";
    state.limit = 100;
    toast("warn","Cleared","Session cleared.");
    route("/settings",{ force:true, soft:true });
  });
}

/* ---------------- Helpers: JSON viewer, CSV, join ---------------- */

function coerceX(v){
  if (v == null) return null;
  if (typeof v === "number" && Number.isFinite(v)) return v;
  const s = String(v).trim();
  if (!s) return null;
  // try date
  const d = new Date(s);
  if (!Number.isNaN(d.getTime())) return d.getTime();
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
}

function predictSeries(points, futureCount){
  if (!points || points.length < 2) return [];
  const n = Math.min(points.length, 20);
  const tail = points.slice(-n);
  const xs = tail.map(p=>p.x);
  const ys = tail.map(p=>p.y);
  const mx = xs.reduce((a,b)=>a+b,0) / xs.length;
  const my = ys.reduce((a,b)=>a+b,0) / ys.length;
  let num = 0, den = 0;
  for (let i=0;i<xs.length;i++){
    const dx = xs[i]-mx;
    num += dx * (ys[i]-my);
    den += dx * dx;
  }
  const slope = den === 0 ? 0 : (num/den);
  const intercept = my - slope * mx;

  // step = median delta
  const deltas = [];
  for (let i=1;i<xs.length;i++) deltas.push(xs[i]-xs[i-1]);
  deltas.sort((a,b)=>a-b);
  const step = deltas.length ? deltas[Math.floor(deltas.length/2)] : 1;
  const start = xs[xs.length-1];

  const out = [];
  for (let i=1;i<=futureCount;i++){
    const x = start + step * i;
    const y = slope * x + intercept;
    out.push({ x, y });
  }
  return out;
}

function drawChart(canvas, series, predictions){
  const ctx = canvas.getContext("2d");
  const w = canvas.width, h = canvas.height;
  ctx.clearRect(0,0,w,h);

  const all = [];
  for (const s of series) all.push(...s.points);
  for (const p of predictions || []) all.push(...p.points);
  if (!all.length){
    ctx.fillStyle = "#a9b5c8";
    ctx.fillText("No data to chart", 20, 30);
    return;
  }

  const xs = all.map(p=>p.x);
  const ys = all.map(p=>p.y);
  const minX = Math.min(...xs), maxX = Math.max(...xs);
  const minY = Math.min(...ys), maxY = Math.max(...ys);
  const pad = 40;

  const sx = (x)=> pad + (x - minX) / (maxX - minX || 1) * (w - pad*2);
  const sy = (y)=> h - pad - (y - minY) / (maxY - minY || 1) * (h - pad*2);

  // grid
  ctx.strokeStyle = "rgba(255,255,255,0.08)";
  ctx.lineWidth = 1;
  for (let i=0;i<=5;i++){
    const y = pad + i * (h - pad*2) / 5;
    ctx.beginPath(); ctx.moveTo(pad, y); ctx.lineTo(w-pad, y); ctx.stroke();
  }
  for (let i=0;i<=5;i++){
    const x = pad + i * (w - pad*2) / 5;
    ctx.beginPath(); ctx.moveTo(x, pad); ctx.lineTo(x, h-pad); ctx.stroke();
  }

  const colors = ["#4ea1ff","#7b5cff","#ff5c7a","#3bd671","#ffcc66","#6bd5ff","#f58d8d","#9dd66b"];

  series.forEach((s, idx)=>{
    const pts = s.points;
    if (!pts.length) return;
    ctx.strokeStyle = colors[idx % colors.length];
    ctx.lineWidth = 2;
    ctx.beginPath();
    pts.forEach((p,i)=>{
      const x = sx(p.x), y = sy(p.y);
      if (i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
    });
    ctx.stroke();
  });

  (predictions || []).forEach((s, idx)=>{
    const pts = s.points;
    if (!pts.length) return;
    ctx.strokeStyle = colors[idx % colors.length];
    ctx.setLineDash([6,4]);
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    pts.forEach((p,i)=>{
      const x = sx(p.x), y = sy(p.y);
      if (i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
    });
    ctx.stroke();
    ctx.setLineDash([]);
  });
}

function renderLegend(root, series, predictions){
  const colors = ["#4ea1ff","#7b5cff","#ff5c7a","#3bd671","#ffcc66","#6bd5ff","#f58d8d","#9dd66b"];
  const rows = [];
  series.forEach((s, idx)=>{
    rows.push(`<span class="badge" style="border-color:${colors[idx%colors.length]}; color:${colors[idx%colors.length]}">● ${esc(s.id)}</span>`);
  });
  if (predictions && predictions.length){
    rows.push(`<span class="badge warn">dashed = prediction</span>`);
  }
  root.innerHTML = rows.join("");
}

function renderInspector(root, obj){
  // Collapsible viewer with truncation to prevent freeze.
  const json = JSON.stringify(obj, null, 2);
  const big = json.length > 12000;
  root.innerHTML = `
    <div class="row" style="justify-content:space-between;">
      <span class="badge ${big ? "warn" : ""}">${big ? "large JSON (truncated preview)" : "JSON"}</span>
      ${big ? `<button class="btn" id="expand">Expand Full</button>` : ""}
    </div>
    <div class="hr"></div>
    <pre id="json">${esc(big ? json.slice(0,12000) + "\n(truncated)" : json)}</pre>
  `;
  if (big){
    $("#expand").addEventListener("click", ()=>{
      $("#json").textContent = json; // safe via textContent
      toast("good","Expanded","Full JSON rendered.");
      $("#expand").remove();
    });
  }
}

function trunc(s,n){
  const t = String(s||"");
  if (t.length <= n) return t;
  return t.slice(0,n) + "";
}

function expandRows(rows, includeMeta){
  const out = [];
  for (const r of rows){
    let d = r.data;
    if (typeof d === "string"){
      try{ d = JSON.parse(d); }catch{ d = null; }
    }
    const items = Array.isArray(d) ? d : (d && typeof d==="object" ? [d] : []);
    for (const rec of items){
      if (!rec || typeof rec!=="object") continue;
      const record = includeMeta ? { _meta:{ id:r.id, drone_id:r.drone_id, profile_id:r.profile_id, timestamp:r.timestamp }, ...rec } : rec;
      out.push({ record });
    }
  }
  return out;
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
    else out[key]=v==null ? "" : String(v);
  }
  return out;
}

function toCsv(rows){
  const flat = rows.map(r=>flatten(r));
  const keys = Array.from(new Set(flat.flatMap(o=>Object.keys(o)))).sort((a,b)=>a.localeCompare(b));
  const escCsv = (s)=>{
    const v = String(s??"");
    if (/[,"\n]/.test(v)) return `"${v.replaceAll('"','""')}"`;
    return v;
  };
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

function joinTwo(A,B, pathA, pathB){
  const mapA = new Map();
  for (const a of A){
    const k = getPath(a, pathA);
    if (k==null) continue;
    const ks = String(k);
    if (!mapA.has(ks)) mapA.set(ks, []);
    mapA.get(ks).push(a);
  }
  const joined = [];
  for (const b of B){
    const k = getPath(b, pathB);
    if (k==null) continue;
    const ks = String(k);
    const as = mapA.get(ks);
    if (!as) continue;
    joined.push({ key: ks, a: as[0], b }); // deterministic: take first match
  }
  joined.sort((x,y)=>String(x.key).localeCompare(String(y.key)));
  return joined;
}

function toNumber(x){
  if (x==null) return null;
  if (typeof x==="number" && Number.isFinite(x)) return x;
  const n = Number(String(x).replaceAll(",","").trim());
  return Number.isFinite(n) ? n : null;
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
  for (let i=0;i<n;i++){
    const vx = xs[i]-mx;
    const vy = ys[i]-my;
    num += vx*vy;
    dx += vx*vx;
    dy += vy*vy;
  }
  const den = Math.sqrt(dx*dy);
  const r = den===0 ? 0 : (num/den);
  return { r, n };
}
