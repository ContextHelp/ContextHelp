// Builds the search-graph viewer into internal/searchgraph/viewer/dist,
// which the Go binary embeds. Run via `make build-searchgraph-viewer`.
//
// Outputs:
//   index.html            page shell (served mode fetches graph.json)
//   viewer.js             IIFE bundle: app + 3d-force-graph + force-graph + three
//   viewer.css            styles
//   viewer.js.LEGAL.txt   third-party license texts + extracted legal comments
import { build } from "esbuild";
import { statSync } from "node:fs";
import { copyFile, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const outdir = resolve(here, "../../internal/searchgraph/viewer/dist");

await rm(outdir, { recursive: true, force: true });
await mkdir(outdir, { recursive: true });

const result = await build({
  absWorkingDir: here,
  entryPoints: { viewer: "src/main.js" },
  outdir,
  bundle: true,
  minify: true,
  format: "iife",
  target: ["es2020"],
  platform: "browser",
  legalComments: "linked",
  metafile: true,
  alias: { "three/webgpu": "./src/webgpu-stub.js" },
  logLevel: "warning",
});

await build({
  absWorkingDir: here,
  entryPoints: { viewer: "src/style.css" },
  outdir,
  bundle: true,
  minify: true,
  logLevel: "warning",
});

await copyFile(join(here, "src/index.html"), join(outdir, "index.html"));

// Collect the npm packages that actually ended up in the bundle and ship
// their license texts next to it, ahead of esbuild's extracted comments.
const packages = new Map();
for (const input of Object.keys(result.metafile.inputs)) {
  const m = input.match(/^(.*node_modules\/)((?:@[^/]+\/)?[^/]+)\//);
  if (m) packages.set(m[2], join(here, m[1], m[2]));
}
const sections = [];
for (const [name, dir] of [...packages].sort(([a], [b]) => (a < b ? -1 : 1))) {
  const pkg = JSON.parse(await readFile(join(dir, "package.json"), "utf8"));
  const files = (await readdir(dir)).filter((f) => /^(licen[cs]e|copying|notice)/i.test(f)).sort();
  let text = "";
  for (const f of files) text += (await readFile(join(dir, f), "utf8")).trim() + "\n";
  if (!text) text = `License: ${pkg.license || "UNKNOWN"} (no license file in package)\n`;
  sections.push(`${name}@${pkg.version} (${pkg.license || "UNKNOWN"})\n${"-".repeat(72)}\n${text}`);
}
const legalPath = join(outdir, "viewer.js.LEGAL.txt");
const extracted = await readFile(legalPath, "utf8").catch(() => "");
const header =
  "Third-party software bundled in viewer.js\n" +
  "=========================================\n\n" +
  "The viewer application code is part of ctxt and is licensed under the\n" +
  "GNU Affero General Public License v3.0. The bundle also contains the\n" +
  "following packages, each under its own license, reproduced below.\n\n";
await writeFile(
  legalPath,
  header +
    sections.join("\n\n") +
    (extracted ? `\n\nLegal comments extracted from the bundled sources\n${"=".repeat(72)}\n${extracted}` : ""),
);

for (const f of (await readdir(outdir)).sort()) {
  console.log(`${relative(process.cwd(), join(outdir, f))}\t${statSync(join(outdir, f)).size}`);
}
