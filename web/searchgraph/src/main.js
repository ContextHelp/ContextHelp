import ForceGraph3D from "3d-force-graph";
import ForceGraph from "force-graph";
import {
  GraphDocumentError,
  STAGES,
  formatNumber,
  shellQuote,
  sortRelations,
  toGraphData,
  tooltipHTML,
  validate,
} from "./jgf.js";
import {
  DEFAULT_CONFIG,
  ConfigError,
  checkLink,
  dataURL,
  fetchFailure,
  objectHref,
  parseConfig,
  signInURL,
} from "./config.js";

const PALETTES = {
  light: {
    background: "#f8fafc",
    kind: { query: "#e11d48", entity: "#8b5cf6", object: "#2563eb", unknown: "#64748b" },
    stage: { returned: "#2563eb", cut_limit: "#d97706", cut_threshold: "#94a3b8" },
    relation: {
      matched: "#94a3b8",
      mentions: "#a78bfa",
      extends: "#0284c7",
      supports: "#059669",
      contradicts: "#dc2626",
      supersedes: "#ea580c",
      "derived-from": "#4f46e5",
      "related-to": "#475569",
      co_mention: "#0d9488",
      similar: "#ca8a04",
    },
    fallback: "#64748b",
  },
  dark: {
    background: "#0b1120",
    kind: { query: "#fb7185", entity: "#a78bfa", object: "#60a5fa", unknown: "#94a3b8" },
    stage: { returned: "#60a5fa", cut_limit: "#fbbf24", cut_threshold: "#64748b" },
    relation: {
      matched: "#475569",
      mentions: "#8b5cf6",
      extends: "#38bdf8",
      supports: "#34d399",
      contradicts: "#f87171",
      supersedes: "#fb923c",
      "derived-from": "#818cf8",
      "related-to": "#94a3b8",
      co_mention: "#2dd4bf",
      similar: "#facc15",
    },
    fallback: "#94a3b8",
  },
};

// Derived relations draw thinner (and dashed in 2D); stored links bolder.
const LINK_STYLE = {
  matched: { width: 0.4, dash: null },
  mentions: { width: 0.6, dash: [2, 2] },
  co_mention: { width: 1, dash: [4, 2] },
  similar: { width: 1, dash: [1, 2] },
};
const STORED_LINK = { width: 1.6, dash: null };

const STAGE_LABELS = { returned: "returned", cut_limit: "cut by limit", cut_threshold: "cut by threshold" };

const $ = (id) => document.getElementById(id);

// The host's configuration, read once on start.
let config = DEFAULT_CONFIG;

const darkQuery = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
const palette = () => (darkQuery && darkQuery.matches ? PALETTES.dark : PALETTES.light);

function showError(message) {
  const el = $("error");
  el.textContent = message;
  el.hidden = false;
}

function showNotice(message) {
  const el = $("notice");
  el.textContent = message;
  el.hidden = false;
}

// showSignIn replaces the generic error with how to sign in: the command,
// copyable, and a link to the host's sign-in page.
function showSignIn({ command, href }) {
  $("signin-cmd").textContent = command;
  $("signin-link").href = href;
  $("signin").hidden = false;
}

// SignInRequired is a 401 from a host that has a sign-in page.
class SignInRequired extends Error {
  constructor(hint) {
    super("sign-in required");
    this.hint = hint;
  }
}

function loadConfig() {
  const el = $("viewer-config");
  const cfg = parseConfig(el ? el.textContent : null);
  checkLink(cfg, window.location.href);
  signInURL(cfg, window.location.href);
  return cfg;
}

async function loadDocument(cfg) {
  const inline = $("graph-data");
  if (inline) {
    try {
      return JSON.parse(inline.textContent);
    } catch (err) {
      throw new GraphDocumentError(`Embedded graph data is not valid JSON: ${err.message}`);
    }
  }
  const name = cfg.dataUrl;
  const url = dataURL(cfg, window.location.href);
  let res;
  try {
    res = await fetch(url, { cache: "no-store" });
  } catch (err) {
    throw new GraphDocumentError(`No embedded graph data, and ${name} could not be fetched: ${err.message}`);
  }
  if (!res.ok) {
    const failure = fetchFailure(cfg, res.status, window.location.href);
    if (failure.signIn) throw new SignInRequired(failure.signIn);
    throw new GraphDocumentError(failure.message);
  }
  try {
    return await res.json();
  } catch (err) {
    throw new GraphDocumentError(`${name} is not valid JSON: ${err.message}`);
  }
}

function hasWebGL() {
  try {
    const c = document.createElement("canvas");
    return !!(c.getContext("webgl2") || c.getContext("webgl"));
  } catch {
    return false;
  }
}

class RendererError extends Error {}

const errText = (err) => (err && err.message ? err.message : String(err));

// createRenderer prefers 3D and falls back to 2D when WebGL is missing or
// the 3D renderer throws while starting (blocklisted GPU, lost context,
// ...). configure(graph, dims) applies data and accessors; its failures
// count as the renderer failing.
function createRenderer(el, configure) {
  let reason = "WebGL is unavailable";
  if (hasWebGL()) {
    let graph;
    try {
      // Default (trackball) controls on purpose: with controlType "orbit",
      // every node click throws in three's OrbitControls when
      // 3d-force-graph replays a synthetic pointerup after the drag ends.
      graph = new ForceGraph3D(el);
      configure(graph, 3);
      return { graph, dims: 3 };
    } catch (err) {
      reason = `3D renderer failed (${errText(err)})`;
      try {
        if (graph) graph._destructor();
      } catch {
        // best effort: the container is cleared below either way
      }
      el.replaceChildren();
    }
  }
  try {
    const graph = new ForceGraph(el);
    configure(graph, 2);
    showNotice(`2D mode: ${reason}.`);
    return { graph, dims: 2 };
  } catch (err) {
    el.replaceChildren();
    throw new RendererError(
      `This browser could not start the graph renderer. ${reason}; the 2D canvas renderer failed too (${errText(err)}).`,
    );
  }
}

function renderFacts(g, data) {
  const m = g.metadata || {};
  $("query").textContent = typeof m.query === "string" ? m.query : typeof g.label === "string" ? g.label : "";
  const c = m.counts && typeof m.counts === "object" ? m.counts : {};
  const pools = [];
  if (m.fts_pool !== undefined) pools.push(`fts ${formatNumber(m.fts_pool)}`);
  if (m.vector_pool !== undefined) pools.push(`vector ${formatNumber(m.vector_pool)}`);
  const entities = data.nodes.filter((n) => n.kind === "entity").length;
  const facts = [
    ["mode", m.mode],
    ["pools", pools.join(" · ")],
    ["threshold", m.threshold !== undefined ? formatNumber(m.threshold) : undefined],
    ["limit", m.limit !== undefined ? formatNumber(m.limit) : undefined],
    ["candidates", c.candidates !== undefined ? formatNumber(c.candidates) : undefined],
    [
      "stages",
      STAGES.filter((s) => c[s] !== undefined)
        .map((s) => `${formatNumber(c[s])} ${s}`)
        .join(" · "),
    ],
    ["nodes", formatNumber(c.nodes !== undefined ? c.nodes : data.nodes.length)],
    ["edges", formatNumber(c.edges !== undefined ? c.edges : data.links.length)],
    ["entities", formatNumber(c.entities !== undefined ? c.entities : entities)],
  ];
  const dl = $("facts");
  for (const [k, v] of facts) {
    if (v === undefined || v === "") continue;
    const div = document.createElement("div");
    const dt = document.createElement("dt");
    const dd = document.createElement("dd");
    dt.textContent = k;
    dd.textContent = String(v);
    div.append(dt, dd);
    dl.append(div);
  }
  const flags = [];
  if (m.truncated === true) flags.push(["truncated", "node or edge cap reached; graph is partial"]);
  if (typeof m.vector_error === "string" && m.vector_error) flags.push(["vector error", m.vector_error]);
  if (data.dropped > 0) flags.push([`${data.dropped} edges skipped`, "edge endpoints missing from nodes"]);
  for (const [text, title] of flags) {
    const div = document.createElement("div");
    const dd = document.createElement("dd");
    dd.className = "badge";
    dd.textContent = text;
    dd.title = title;
    div.append(dd);
    dl.append(div);
  }
}

function toggle(container, { label, count, swatch, checked, onChange }) {
  const lab = document.createElement("label");
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = checked;
  input.addEventListener("change", () => onChange(input.checked));
  const sw = document.createElement("span");
  sw.className = swatch.className;
  swatch.el = sw;
  const text = document.createElement("span");
  text.textContent = label;
  const cnt = document.createElement("span");
  cnt.className = "count";
  cnt.textContent = String(count);
  lab.append(input, sw, text, cnt);
  container.append(lab);
  return lab;
}

function main(doc) {
  const g = validate(doc);
  const data = toGraphData(g);
  renderFacts(g, data);

  const state = { stages: {}, relations: {}, kinds: { query: true, object: true, entity: true } };
  const byId = new Map(data.nodes.map((n) => [n.id, n]));
  const endpoint = (x) => (typeof x === "object" && x !== null ? x : byId.get(x));

  const nodeVisible = (n) =>
    state.kinds[n.kind] !== false && (n.kind !== "object" || state.stages[n.stage] !== false);
  const linkVisible = (l) => {
    if (state.relations[l.relation] === false) return false;
    const s = endpoint(l.source);
    const t = endpoint(l.target);
    return !!s && !!t && nodeVisible(s) && nodeVisible(t);
  };

  const nodeColor = (n) => {
    const p = palette();
    if (n.kind === "object" && p.stage[n.stage]) return p.stage[n.stage];
    return p.kind[n.kind] || p.fallback;
  };
  const linkColor = (l) => palette().relation[l.relation] || palette().fallback;
  const linkStyle = (l) => LINK_STYLE[l.relation] || STORED_LINK;

  const maxTotal = data.nodes.reduce((m, n) => Math.max(m, scoreTotal(n)), 0);
  const nodeVal = (n) => {
    if (n.kind === "query") return 12;
    if (n.kind === "entity") return 1.5;
    return 1 + (maxTotal > 0 ? (9 * scoreTotal(n)) / maxTotal : 0);
  };

  const el = $("graph");
  let fitted = false;
  const configure = (graph, dims) => {
    // Anchor the query at the origin so results arrange around it.
    for (const n of data.nodes) {
      if (n.kind !== "query") continue;
      n.fx = 0;
      n.fy = 0;
      if (dims === 3) n.fz = 0;
    }
    graph
      .width(el.clientWidth || window.innerWidth)
      .height(el.clientHeight || window.innerHeight)
      .backgroundColor(palette().background)
      .nodeId("id")
      .nodeVal(nodeVal)
      .nodeRelSize(4)
      .nodeColor(nodeColor)
      .nodeLabel(tooltipHTML)
      .nodeVisibility(nodeVisible)
      .linkVisibility(linkVisible)
      .linkColor(linkColor)
      .linkWidth((l) => linkStyle(l).width)
      .linkCurvature((l) => l.curvature || 0)
      .linkDirectionalArrowLength((l) => (l.directed ? (dims === 3 ? 3 : 4) : 0))
      .linkDirectionalArrowRelPos(1)
      .onNodeClick(showDetails)
      // Mostly settle before the first frame, then frame the result once
      // the simulation stops; later stops (after drags) keep the user's view.
      .warmupTicks(80)
      .cooldownTime(4000)
      .onEngineStop(() => {
        if (fitted) return;
        fitted = true;
        graph.zoomToFit(400, 90);
      });
    if (dims === 2) graph.linkLineDash((l) => linkStyle(l).dash);
    graph.graphData({ nodes: data.nodes, links: data.links });
  };
  const { graph, dims } = createRenderer(el, configure);
  document.body.dataset.renderer = `${dims}d`;
  if (dims === 2) $("hint").textContent = "Drag to pan, scroll to zoom, click an object for its id.";

  const refresh = () => {
    // Re-setting an accessor makes the renderer re-evaluate it without
    // touching the layout.
    graph.nodeVisibility(nodeVisible).linkVisibility(linkVisible);
  };
  const recolor = () => {
    graph.backgroundColor(palette().background).nodeColor(nodeColor).linkColor(linkColor);
    paintSwatches();
  };

  // Legend + filters.
  const swatches = [];
  const addSwatch = (sw, color) => swatches.push({ sw, color });
  const paintSwatches = () => {
    for (const { sw, color } of swatches) {
      const c = color();
      sw.el.style.background = c;
      sw.el.style.borderColor = c;
    }
  };

  const kindCounts = countBy(data.nodes, (n) => n.kind);
  for (const kind of ["query", "object", "entity"]) {
    if (!kindCounts.has(kind)) continue;
    const sw = { className: "swatch" };
    addSwatch(sw, () => palette().kind[kind]);
    if (kind === "query") {
      const lab = toggle($("kinds"), { label: kind, count: kindCounts.get(kind), swatch: sw, checked: true, onChange: () => {} });
      lab.querySelector("input").disabled = true;
      continue;
    }
    toggle($("kinds"), {
      label: kind === "object" ? "objects" : "entities",
      count: kindCounts.get(kind),
      swatch: sw,
      checked: true,
      onChange: (on) => {
        state.kinds[kind] = on;
        refresh();
      },
    });
  }

  const stageCounts = countBy(
    data.nodes.filter((n) => n.kind === "object"),
    (n) => n.stage,
  );
  const stages = [...STAGES, ...[...stageCounts.keys()].filter((s) => typeof s === "string" && !STAGES.includes(s)).sort()];
  for (const stage of stages) {
    const sw = { className: "swatch" };
    addSwatch(sw, () => palette().stage[stage] || palette().kind.object);
    toggle($("stages"), {
      label: STAGE_LABELS[stage] || String(stage),
      count: stageCounts.get(stage) || 0,
      swatch: sw,
      checked: true,
      onChange: (on) => {
        state.stages[stage] = on;
        refresh();
      },
    });
  }

  const relCounts = countBy(data.links, (l) => l.relation);
  for (const rel of sortRelations(relCounts.keys())) {
    const sw = { className: `swatch line${linkStyle({ relation: rel }).dash ? " dashed" : ""}` };
    addSwatch(sw, () => palette().relation[rel] || palette().fallback);
    toggle($("relations"), {
      label: rel,
      count: relCounts.get(rel),
      swatch: sw,
      checked: true,
      onChange: (on) => {
        state.relations[rel] = on;
        refresh();
      },
    });
  }
  paintSwatches();

  if (darkQuery) {
    const onScheme = () => recolor();
    if (darkQuery.addEventListener) darkQuery.addEventListener("change", onScheme);
    else if (darkQuery.addListener) darkQuery.addListener(onScheme);
  }
  window.addEventListener("resize", () => {
    graph.width(el.clientWidth || window.innerWidth).height(el.clientHeight || window.innerHeight);
  });

  // Exposed for debugging from the browser console; read-only by convention.
  window.ctxtSearchGraph = { graph, data, dims };
}

function scoreTotal(n) {
  const v = n.score && n.score.total;
  return typeof v === "number" && Number.isFinite(v) && v > 0 ? v : 0;
}

function countBy(items, key) {
  const m = new Map();
  for (const it of items) {
    const k = key(it);
    m.set(k, (m.get(k) || 0) + 1);
  }
  return m;
}

function showDetails(n) {
  if (!n) return;
  $("details-kind").textContent = [n.kind, n.stage, n.rank !== undefined ? `rank ${formatNumber(n.rank)}` : ""]
    .filter(Boolean)
    .join(" · ");
  $("details-label").textContent = n.label;
  const obj = $("details-object");
  let focus = $("details-close");
  if (n.kind === "object" && typeof n.object_id === "string" && n.object_id !== "") {
    $("details-id").textContent = n.object_id;
    const cmdRow = $("details-cmd-row");
    const linkRow = $("details-link-row");
    cmdRow.hidden = true;
    linkRow.hidden = true;
    if (config.objectAction === "link") {
      const href = objectHref(config, n.object_id, window.location.href);
      const a = $("details-link");
      if (href) {
        a.href = href;
        linkRow.hidden = false;
        focus = a;
      } else {
        a.removeAttribute("href");
      }
    } else {
      $("details-cmd").textContent = `ctxt show ${shellQuote(n.object_id)}`;
      $("details-copied").textContent = "";
      cmdRow.hidden = false;
      focus = $("details-copy");
    }
    obj.hidden = false;
  } else {
    obj.hidden = true;
  }
  $("details").hidden = false;
  focus.focus({ preventScroll: true });
}

// copyCommand copies the text of the code element cmdId and reports it in
// the status element statusId.
async function copyCommand(cmdId, statusId) {
  const text = $(cmdId).textContent;
  const status = $(statusId);
  try {
    await navigator.clipboard.writeText(text);
    status.textContent = "Copied";
  } catch {
    // Clipboard API unavailable (e.g. file:// in some browsers): select the
    // text so the user can copy it by hand.
    const range = document.createRange();
    range.selectNodeContents($(cmdId));
    const sel = window.getSelection();
    sel.removeAllRanges();
    sel.addRange(range);
    status.textContent = "Selected: press Ctrl/Cmd+C";
  }
}

function hideDetails() {
  $("details").hidden = true;
}

function start() {
  $("details-close").addEventListener("click", hideDetails);
  $("details-copy").addEventListener("click", () => copyCommand("details-cmd", "details-copied"));
  $("signin-copy").addEventListener("click", () => copyCommand("signin-cmd", "signin-copied"));
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") hideDetails();
  });
  Promise.resolve()
    .then(() => {
      config = loadConfig();
      return loadDocument(config);
    })
    .then(main)
    .catch((err) => {
      if (err instanceof SignInRequired) {
        showSignIn(err.hint);
      } else if (err instanceof GraphDocumentError || err instanceof RendererError || err instanceof ConfigError) {
        showError(err.message);
      } else {
        showError(`The search graph could not be rendered: ${errText(err)}`);
        console.error(err);
      }
    });
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", start);
} else {
  start();
}
