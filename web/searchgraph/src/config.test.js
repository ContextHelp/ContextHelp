// Unit tests for the host configuration. Run: pnpm test (node --test).
import assert from "node:assert/strict";
import { test } from "node:test";
import { ConfigError, DEFAULT_CONFIG, checkLink, dataURL, objectHref, parseConfig } from "./config.js";

const PAGE = "http://127.0.0.1:8123/ui/searchgraph/?q=deploy%20x&limit=5";

test("no config element means today's behaviour", () => {
  const cfg = parseConfig(null);
  assert.deepEqual(cfg, { dataUrl: "graph.json", forwardQuery: false, objectAction: "copy-cli", objectHref: "" });
  assert.equal(dataURL(cfg, "http://127.0.0.1:9/tok/"), "http://127.0.0.1:9/tok/graph.json");
  // The page's query string is not forwarded by default.
  assert.equal(dataURL(cfg, "http://127.0.0.1:9/tok/?q=a"), "http://127.0.0.1:9/tok/graph.json");
});

test("missing fields take defaults", () => {
  assert.deepEqual(parseConfig("{}"), { ...DEFAULT_CONFIG });
  assert.equal(parseConfig('{"forwardQuery":true}').dataUrl, "graph.json");
});

test("malformed configs are rejected", () => {
  for (const text of [
    "",
    "[",
    "[]",
    "null",
    '{"dataUrl":1}',
    '{"forwardQuery":"yes"}',
    '{"dataUrl":""}',
    '{"objectAction":"exec"}',
    '{"objectAction":"link"}',
    '{"objectAction":"link","objectHref":"/ui/objects/"}',
  ]) {
    assert.throws(() => parseConfig(text), ConfigError, text);
  }
});

test("forwardQuery carries the page query, host parameters win", () => {
  const cfg = parseConfig('{"dataUrl":"/api/v1/search/graph","forwardQuery":true}');
  assert.equal(dataURL(cfg, PAGE), "http://127.0.0.1:8123/api/v1/search/graph?q=deploy+x&limit=5");
  const fixed = parseConfig('{"dataUrl":"/api/v1/search/graph?limit=50","forwardQuery":true}');
  assert.equal(dataURL(fixed, PAGE), "http://127.0.0.1:8123/api/v1/search/graph?limit=50&q=deploy+x");
  const repeated = parseConfig('{"dataUrl":"/g","forwardQuery":true}');
  assert.equal(dataURL(repeated, "http://h/p?t=a&t=b"), "http://h/g?t=a&t=b");
});

test("data URLs off the page's origin are refused", () => {
  for (const dataUrl of [
    "https://evil.example/g",
    "//evil.example/g",
    "/\\evil.example/g",
    "\\\\evil.example/g",
    "http://127.0.0.1:8124/g",
    "https://127.0.0.1:8123/g",
    "javascript:alert(1)",
    "data:application/json,{}",
  ]) {
    const cfg = parseConfig(JSON.stringify({ dataUrl, forwardQuery: true }));
    assert.throws(() => dataURL(cfg, PAGE), ConfigError, dataUrl);
  }
  // A file:// page has no origin to share: fetching is refused outright.
  assert.throws(() => dataURL(parseConfig(null), "file:///tmp/graph.html"), ConfigError);
});

test("link mode encodes the id into a same-origin path", () => {
  const cfg = parseConfig('{"objectAction":"link","objectHref":"/ui/objects/{id}"}');
  checkLink(cfg, PAGE);
  assert.equal(objectHref(cfg, "01JABC", PAGE), "http://127.0.0.1:8123/ui/objects/01JABC");
  assert.equal(
    objectHref(cfg, "a/../b c?d#e&f", PAGE),
    "http://127.0.0.1:8123/ui/objects/a%2F..%2Fb%20c%3Fd%23e%26f",
  );
  // Protocol-relative or scheme-bearing ids cannot escape: they are one encoded segment.
  assert.equal(objectHref(cfg, "//evil.example", PAGE), "http://127.0.0.1:8123/ui/objects/%2F%2Fevil.example");
  for (const id of ["", ".", ".."]) assert.equal(objectHref(cfg, id, PAGE), "", id);
  const query = parseConfig('{"objectAction":"link","objectHref":"/ui/objects?id={id}&from={id}"}');
  assert.equal(objectHref(query, "x y", PAGE), "http://127.0.0.1:8123/ui/objects?id=x%20y&from=x%20y");
});

test("link templates off the page's origin are refused", () => {
  for (const objectHref of [
    "https://evil.example/{id}",
    "//evil.example/{id}",
    "/\\evil.example/{id}",
    "javascript:alert('{id}')",
    "{id}:x",
  ]) {
    const cfg = parseConfig(JSON.stringify({ objectAction: "link", objectHref }));
    assert.throws(() => checkLink(cfg, PAGE), ConfigError, objectHref);
  }
  // An id that would form a scheme at the template's start still cannot leave.
  const bare = parseConfig('{"objectAction":"link","objectHref":"{id}"}');
  assert.equal(objectHref(bare, "javascript:alert(1)", PAGE), "http://127.0.0.1:8123/ui/searchgraph/javascript%3Aalert(1)");
});

test("copy-cli mode never checks the link template", () => {
  checkLink(parseConfig('{"objectHref":"https://evil.example/{id}"}'), PAGE);
});
