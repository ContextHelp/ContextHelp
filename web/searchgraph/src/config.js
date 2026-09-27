// Host configuration: where the page loads its graph document, what a
// click on an object offers, and where a signed-out browser signs in. The
// host (ctxt's local server, or dpkms) injects it as
// <script type="application/json" id="viewer-config">; with no such
// element the defaults apply, which is how the CLI serves the page. Nothing here reads the page's own URL except to forward its query
// string to a data URL the host already fixed. No DOM access here.

export const OBJECT_ACTIONS = ["copy-cli", "link"];

export const ID_PLACEHOLDER = "{id}";

export const DEFAULT_CONFIG = Object.freeze({
  dataUrl: "graph.json",
  forwardQuery: false,
  objectAction: "copy-cli",
  objectHref: "",
  signInHref: "",
});

// The command that signs a browser in to a dpkms instance.
export const SIGN_IN_COMMAND = "ctxt ui open";

export class ConfigError extends Error {}

const isObject = (v) => v !== null && typeof v === "object" && !Array.isArray(v);

// parseConfig reads the host's JSON config text; null or undefined text
// means no config element, i.e. the defaults. Missing fields take their
// default; a field of the wrong type or an unknown action is an error.
export function parseConfig(text) {
  if (text === null || text === undefined) return { ...DEFAULT_CONFIG };
  let raw;
  try {
    raw = JSON.parse(text);
  } catch (err) {
    throw new ConfigError(`Viewer configuration is not valid JSON: ${err.message}`);
  }
  if (!isObject(raw)) throw new ConfigError("Viewer configuration must be a JSON object.");
  const cfg = { ...DEFAULT_CONFIG };
  for (const key of Object.keys(DEFAULT_CONFIG)) {
    if (raw[key] === undefined) continue;
    if (typeof raw[key] !== typeof DEFAULT_CONFIG[key]) {
      throw new ConfigError(`Viewer configuration: ${key} must be a ${typeof DEFAULT_CONFIG[key]}.`);
    }
    cfg[key] = raw[key];
  }
  if (cfg.dataUrl === "") throw new ConfigError("Viewer configuration: dataUrl is empty.");
  if (!OBJECT_ACTIONS.includes(cfg.objectAction)) {
    throw new ConfigError(`Viewer configuration: unknown objectAction "${cfg.objectAction}".`);
  }
  if (cfg.objectAction === "link" && !cfg.objectHref.includes(ID_PLACEHOLDER)) {
    throw new ConfigError(`Viewer configuration: objectHref must contain ${ID_PLACEHOLDER}.`);
  }
  return cfg;
}

// sameOrigin resolves ref against pageHref and returns the URL, or throws
// unless it is an http(s) URL on the page's own origin.
function sameOrigin(ref, pageHref, what) {
  const page = new URL(pageHref);
  let url;
  try {
    url = new URL(ref, page);
  } catch {
    throw new ConfigError(`Viewer configuration: ${what} is not a URL.`);
  }
  if ((url.protocol !== "http:" && url.protocol !== "https:") || url.origin !== page.origin) {
    throw new ConfigError(`Viewer configuration: ${what} must stay on this page's origin.`);
  }
  return url;
}

// checkLink fails a "link" config whose template leaves the page's
// origin, so a bad host config shows up on load rather than as links
// that silently never appear.
export function checkLink(cfg, pageHref) {
  if (cfg.objectAction !== "link") return;
  sameOrigin(cfg.objectHref.split(ID_PLACEHOLDER).join("id"), pageHref, "objectHref");
}

// dataURL is the URL the page fetches its document from. With
// forwardQuery, every page query parameter the host's dataUrl does not
// already set is appended, so the host's own parameters always win.
export function dataURL(cfg, pageHref) {
  const url = sameOrigin(cfg.dataUrl, pageHref, "dataUrl");
  if (cfg.forwardQuery) {
    const fixed = new Set(url.searchParams.keys());
    for (const [k, v] of new URL(pageHref).searchParams) {
      if (!fixed.has(k)) url.searchParams.append(k, v);
    }
  }
  return url.href;
}

// signInURL is the host's sign-in page, or "" when the host names none.
// A sign-in page off the page's origin is an error.
export function signInURL(cfg, pageHref) {
  if (cfg.signInHref === "") return "";
  return sameOrigin(cfg.signInHref, pageHref, "signInHref").href;
}

// fetchFailure is what the page shows when the data URL answers status
// (not 2xx): a sign-in hint for a 401 from a host with a sign-in page,
// i.e. a dpkms instance the browser holds no session for, otherwise the
// generic message.
export function fetchFailure(cfg, status, pageHref) {
  if (status === 401 && cfg.signInHref !== "") {
    return { signIn: { command: SIGN_IN_COMMAND, href: signInURL(cfg, pageHref) } };
  }
  return { message: `No embedded graph data, and ${cfg.dataUrl} returned HTTP ${status}.` };
}

// objectHref is the link for an object id in "link" mode: the host's
// template with every {id} replaced by the URL-encoded id. It returns ""
// when the id cannot make a distinct path segment ("." and ".." would
// resolve away) or the result leaves the origin.
export function objectHref(cfg, id, pageHref) {
  const s = String(id);
  if (s === "" || s === "." || s === "..") return "";
  const ref = cfg.objectHref.split(ID_PLACEHOLDER).join(encodeURIComponent(s));
  try {
    return sameOrigin(ref, pageHref, "objectHref").href;
  } catch {
    return "";
  }
}
