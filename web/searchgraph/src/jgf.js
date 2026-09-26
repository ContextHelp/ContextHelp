// JGF v2.1 search-graph document helpers: validation, mapping to the
// renderer's {nodes, links} shape, and HTML escaping. No DOM access here.

export const VOCABULARY_PREFIX = "ctxt.search-graph/";

export const STAGES = ["returned", "cut_limit", "cut_threshold"];

// Display order for relation toggles; relations not listed here sort after
// these, alphabetically.
export const RELATIONS = [
  "matched",
  "mentions",
  "extends",
  "supports",
  "contradicts",
  "supersedes",
  "derived-from",
  "related-to",
  "co_mention",
  "similar",
];

export const SCORE_KEYS = [
  "fts",
  "vector",
  "mention_boost",
  "graph_relevance",
  "word_overlap",
  "total",
];

export class GraphDocumentError extends Error {}

const isObject = (v) => v !== null && typeof v === "object" && !Array.isArray(v);

// validate throws GraphDocumentError unless doc is a single-graph JGF
// document carrying the ctxt search-graph vocabulary.
export function validate(doc) {
  if (!isObject(doc) || !isObject(doc.graph)) {
    throw new GraphDocumentError(
      "Not a JSON Graph Format document: expected a top-level \"graph\" object.",
    );
  }
  const g = doc.graph;
  const vocab = isObject(g.metadata) ? g.metadata.vocabulary : undefined;
  if (typeof vocab !== "string" || !vocab.startsWith(VOCABULARY_PREFIX)) {
    const got = typeof vocab === "string" ? `"${vocab}"` : "nothing";
    throw new GraphDocumentError(
      `Not a ctxt search graph: graph.metadata.vocabulary must start with ` +
        `"${VOCABULARY_PREFIX}", found ${got}.`,
    );
  }
  if (!isObject(g.nodes)) {
    throw new GraphDocumentError("Malformed search graph: graph.nodes must be an object keyed by node id.");
  }
  if (g.edges !== undefined && !Array.isArray(g.edges)) {
    throw new GraphDocumentError("Malformed search graph: graph.edges must be an array.");
  }
  return g;
}

// toGraphData maps a validated graph to renderer input. Edges whose
// endpoints are not nodes are dropped (the renderers reject them).
// Parallel edges between the same pair get distinct curvatures.
export function toGraphData(g) {
  const nodes = Object.entries(g.nodes).map(([id, n]) => {
    const meta = isObject(n) && isObject(n.metadata) ? n.metadata : {};
    const label = isObject(n) && typeof n.label === "string" && n.label !== "" ? n.label : id;
    return { ...meta, id, label, kind: typeof meta.kind === "string" ? meta.kind : "unknown" };
  });
  const ids = new Set(nodes.map((n) => n.id));
  const links = [];
  let dropped = 0;
  for (const e of g.edges || []) {
    if (!isObject(e) || !ids.has(e.source) || !ids.has(e.target)) {
      dropped++;
      continue;
    }
    const meta = isObject(e.metadata) ? e.metadata : {};
    links.push({
      ...meta,
      source: e.source,
      target: e.target,
      relation: typeof e.relation === "string" ? e.relation : "unknown",
      directed: typeof e.directed === "boolean" ? e.directed : g.directed === true,
    });
  }
  assignCurvature(links);
  return { nodes, links, dropped };
}

function assignCurvature(links) {
  const byPair = new Map();
  for (const l of links) {
    const key = l.source < l.target ? `${l.source}\u0000${l.target}` : `${l.target}\u0000${l.source}`;
    if (!byPair.has(key)) byPair.set(key, []);
    byPair.get(key).push(l);
  }
  for (const group of byPair.values()) {
    if (group.length < 2) continue;
    group.forEach((l, i) => {
      // 0, +0.25, -0.25, +0.5, ...; sign flips with direction so curves
      // for opposite-direction edges still separate.
      const step = Math.ceil(i / 2) * 0.25 * (i % 2 ? 1 : -1);
      l.curvature = l.source < l.target ? step : -step;
    });
  }
}

const HTML_ESCAPES = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

export function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (c) => HTML_ESCAPES[c]);
}

export function formatNumber(v, digits = 4) {
  if (typeof v !== "number" || !Number.isFinite(v)) return "–";
  if (Number.isInteger(v)) return String(v);
  return Number(v.toFixed(digits)).toString();
}

// shellQuote returns id as-is when it is shell-safe, else single-quoted.
export function shellQuote(id) {
  const s = String(id);
  if (/^[A-Za-z0-9._:@%+=,/-]+$/.test(s)) return s;
  return `'${s.replace(/'/g, "'\\''")}'`;
}

export function sortRelations(list) {
  const known = new Map(RELATIONS.map((r, i) => [r, i]));
  return [...list].sort((a, b) => {
    const ia = known.has(a) ? known.get(a) : RELATIONS.length;
    const ib = known.has(b) ? known.get(b) : RELATIONS.length;
    return ia - ib || (a < b ? -1 : a > b ? 1 : 0);
  });
}

// tooltipHTML renders the hover card. Every interpolated value is escaped:
// labels and metadata come from user data.
export function tooltipHTML(n) {
  const e = escapeHTML;
  const meta = [e(n.kind)];
  if (n.kind === "object") {
    if (n.stage) meta.push(e(n.stage));
    if (n.rank !== undefined) meta.push(`rank ${e(formatNumber(n.rank))}`);
    if (n.legs) meta.push(`legs ${e(n.legs)}`);
  } else if (n.kind === "entity" && n.mention_count !== undefined) {
    meta.push(`${e(formatNumber(n.mention_count))} mentions`);
  }
  let rows = "";
  if (isObject(n.score)) {
    rows = SCORE_KEYS.map(
      (k) =>
        `<tr${k === "total" ? ' class="total"' : ""}><th>${e(k)}</th><td>${e(formatNumber(n.score[k]))}</td></tr>`,
    ).join("");
    rows = `<table>${rows}</table>`;
  }
  return (
    `<div class="tt"><div class="tt-label">${e(n.label)}</div>` +
    `<div class="tt-meta">${meta.join(" · ")}</div>${rows}</div>`
  );
}
