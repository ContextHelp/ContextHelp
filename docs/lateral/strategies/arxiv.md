# ArxivStrategy

Single platform-keyed strategy for arxiv.org.

## Surfaces

| Host | Specificity |
|---|---|
| arxiv.org | 1 |
| any *.arxiv.org (mirror hosts) | 2 |

## Sub-paths

- Recognised surfaces: `/abs/<id>`, `/pdf/<id>`, `/html/<id>`,
  `/format/<id>`, `/ps/<id>`.
- `arxiv_paper` — canonical `/abs/<id>` link.
- `arxiv_pdf` — canonical `/pdf/<id>` link.
- `arxiv_version` — emitted only when the captured URL carries a `vN`
  suffix.
- `arxiv_author` — one per author returned by `ArxivClient.ListAuthors`.
- `arxiv_mirror` — Semantic Scholar lookup URL.

ID forms supported: modern `NNNN.NNNNN[vN]` and legacy
`<archive>/<7digits>` (e.g. `cs.LG/0303001`).

## Identity keys

| Type | Form |
|---|---|
| `arxiv_paper` / `arxiv_pdf` / `arxiv_mirror` | `arxiv/paper/<id>` |
| `arxiv_version` | `arxiv/paper/<id>v<N>` |
| `arxiv_author` | `arxiv/profile/<slug>` |

## Wiring

`arxiv.New(client)` takes an `ArxivClient.ListAuthors`. Without it the
strategy still emits paper / pdf / mirror probes and version (when
present) — only authors are skipped.

## Limits

- Author slug derivation is naive (`strings.ReplaceAll(name, " ",
  "_")`). The daemon-side client should canonicalise where it has
  better information.
- No citation/reference extraction in v1; the Semantic Scholar mirror
  candidate is the lateral hook into that graph.
