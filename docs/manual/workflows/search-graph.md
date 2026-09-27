# Workflow: See What a Search Considered

## Goal

See every candidate a `ctxt find` search looked at, not just the results it printed: which objects it returned, which it cut and why, how each one scored, and how they connect to each other and to the entities they mention.

Use it when a result you expected is missing, when the order looks wrong, or when you want to explore a topic's neighborhood visually.

## Quick start

```bash
ctxt find "deployment" --graph
```

ctxt runs the search, starts a small viewer on your machine and opens it in your browser. On stderr you'll see:

```text
Search graph viewer: http://127.0.0.1:50542/kPqc3HxlvnpoT-ZMKV8oVf8-uaTCok0TMrizTted0aM/
Serving on loopback only; stops after 10m0s without a request. Press Ctrl-C to stop.
```

Press **Ctrl-C** when you're done:

```text
Search graph viewer stopped (interrupted).
```

The viewer also stops by itself after 10 minutes without a request. The port and the token in the URL change on every run; only that exact URL answers.

No browser window? Open the printed URL yourself. ctxt only opens a browser when you run it in a terminal; to never open one, add `--no-browser`:

```bash
ctxt find "deployment" --graph --no-browser
```

## Read the graph

The page header summarizes the search: mode, candidate pools, score threshold, limit, how many candidates there were, and how many were returned or cut.

### Nodes

| Node | Looks like | Meaning |
|---|---|---|
| Query | red, at the center | The text you searched for |
| Returned object | blue | Made it into the results `ctxt find` prints |
| Cut by limit | amber | Scored well enough but fell past `--limit` |
| Cut by threshold | grey | Scored below the minimum score (`--min-score`) |
| Entity | small purple | A person, team, tool, project or concept a candidate mentions |

Object nodes are sized by their final score: the bigger the node, the higher it ranked.

### Edges

| Edge | Between | Meaning |
|---|---|---|
| `matched` | query → object | The search surfaced this object; weight is its final score |
| `mentions` | object → entity | The object mentions the entity |
| `extends`, `contradicts`, `supersedes`, `supports`, `derived-from` | object → object | A link stored with `ctxt link create` |
| `related-to` | object — object | A stored link with no direction |
| `co_mention` | object — object | The two objects mention the same entities; weight is how many they share |
| `similar` | object — object | The two objects' embeddings under the default embedding model are close (only with `--graph-similar`) |

### Interact

- **Hover** an object to see its stage, rank, which search legs found it (`fts`, `vector` or `both`) and its per-signal scores: `fts`, `vector`, `mention_boost`, `graph_relevance`, `word_overlap`, `total`.
- **Click** an object to open its details: the object id and a `ctxt show <id>` command with a **Copy** button.
- **Toggle** node kinds, stages and edge types in the side panel to declutter. Unticking *cut by limit* and *entities*, for example, leaves only the returned results and the links between them.
- **Drag** to rotate, **scroll** to zoom.

The viewer draws in 3D with WebGL. Without WebGL it switches to a 2D view and says so at the top: `2D mode: WebGL is unavailable.`

## Save or share a graph

Give `-o` a file name; the extension picks the format:

```bash
ctxt find "deployment" --graph -o deployment.html
```

```text
Wrote search graph (standalone HTML viewer) to deployment.html
```

| Extension | Format | Open it with |
|---|---|---|
| `.html` | The same viewer in one self-contained file | Any browser, offline; nothing is loaded from the network |
| `.json` | JSON Graph Format (JGF) v2.1 | `jq`, scripts, any JGF tool |
| `.yaml`, `.yml` | The same document as YAML | Editors, scripts |
| `.graphml` | GraphML | Gephi, yEd, Cytoscape, NetworkX, igraph |
| `.gexf` | GEXF 1.3, with node colors and sizes | Gephi, graphology |

Notes:

- Files are written with mode `0600` (readable only by you) because they contain your knowledge. Keep that in mind before you share one.
- GraphML graphs are directed; each edge carries a `directed` attribute so you can tell the undirected ones (`related-to`, `co_mention`, `similar`) apart. Nested fields are flattened: `score.total` becomes `score_total`.
- NetworkX can't read GEXF 1.3. Use the `.graphml` file there:

  ```python
  import networkx as nx
  g = nx.read_graphml("deployment.graphml")   # MultiDiGraph
  ```

## Use the graph as data

Print the graph to stdout instead of opening the viewer:

```bash
ctxt find "deployment" --graph --format json
ctxt find "deployment" --graph --format yaml
```

The output is a single-graph JGF v2.1 document: `{"graph": {"nodes": {...}, "edges": [...]}}`. Nodes are keyed by id: `query`, `obj:<object id>` and `ent:<entity>`. Everything ctxt adds lives under `metadata`; fields that don't apply are left out.

### List the candidates the search cut

Best first:

```bash
ctxt find "deployment" --graph --format json | jq -r '
  [.graph.nodes[] | select(.metadata.stage == "cut_limit" or .metadata.stage == "cut_threshold")]
  | sort_by(.metadata.rank)[]
  | [.metadata.rank, .metadata.stage, .metadata.score.total, .metadata.object_id] | @tsv' \
  | column -t
```

```text
11  cut_limit  0.007692307692307693   b1581fe7-ec5c-4ae4-9b6a-e0f14e0f772a
12  cut_limit  0.007462686567164179   f71b2e71-9249-4d39-a7e1-855be45e866f
13  cut_limit  0.007352941176470588   3c720900-af59-438d-9ce7-31a0ca48ecdc
```

### Useful keys

| Where | Key | Meaning |
|---|---|---|
| `graph.metadata` | `vocabulary` | Always `ctxt.search-graph/v1` for this document shape |
| | `mode` | `hybrid`; `fts_only` when there is no default embedding model; `fts_fallback` when the vector search couldn't run (see `vector_error`) |
| | `vector_model` | The default embedding model the vector search read; `similar` edges compare its vectors |
| | `semantic_status` | `ok`, or why the vector search added nothing: the same status as the `notice:` line and `ctxt find --format json`'s `diagnostics.semantic` |
| | `fts_pool`, `vector_pool` | How many candidates each search leg could contribute |
| | `limit`, `threshold` | The result limit and minimum score in effect |
| | `counts` | `candidates`, `returned`, `cut_limit`, `cut_threshold`, hits per leg, and the `nodes`, `edges`, `entities` in this document |
| | `truncated` | `true` when a size cap left something out (see below) |
| object node `metadata` | `stage` | `returned`, `cut_limit` or `cut_threshold` |
| | `legs` | `fts`, `vector` or `both`: which search legs found it |
| | `rank` | Final rank, starting at 1 |
| | `score.fts`, `score.vector`, `score.mention_boost`, `score.graph_relevance`, `score.word_overlap`, `score.total` | The per-signal score breakdown |
| | `object_id` | Pass it to `ctxt show` |
| edge | `relation`, `directed`, `metadata.weight` | Edge type, direction and weight (`matched`, `co_mention`, `similar`) |

## Tune the graph

`--limit`, `--min-score` and the metadata filters (`--topic`, `--since`, `--source-type`, ...) apply as usual; they change which candidates are returned or cut, and the graph shows the result.

| Flag | Default | Effect |
|---|---|---|
| `--graph-max-nodes` | `250` | Node cap, query included |
| `--graph-max-edges` | `1500` | Edge cap |
| `--graph-similar` | off | Add `similar` edges between candidates, from their stored vectors under the default embedding model |
| `--graph-similar-threshold` | `0.8` | Minimum cosine similarity for a `similar` edge (with `--graph-similar`) |
| `--no-browser` | off | Print the viewer URL without opening a browser |
| `--graph-idle-timeout` | `10m` | Stop the viewer after this long without a request |

When a cap is hit, ctxt keeps the query, then returned objects, then objects cut by limit, then by threshold, then the most-mentioned entities, and sets `metadata.truncated` to `true`. The viewer shows a **truncated** badge in its header.

```bash
ctxt find "deployment" --graph --graph-max-nodes 50 --graph-similar -o deployment.html
```

## Limits

- The graph always shows the full hybrid search, so `--graph` can't be combined with `--fts`, `--semantic`, `--explain` or `--facets`:

  ```text
  USAGE: --graph cannot be combined with --fts: the graph is built from the hybrid search trace
  ```

- `--format` accepts `json` or `yaml` with `--graph`; for other formats use `-o` with a graph extension.
- When the vector search can't run (no default embedding model, the provider can't be reached, the model has no index or no vectors yet), the search falls back to full-text only, as plain `ctxt find` does, and prints the same `notice:` line on stderr. The graph still works: `metadata.mode` is `fts_only` or `fts_fallback`, `metadata.semantic_status` names the reason, `metadata.vector_error` explains it, and the viewer shows a **vector error** badge. Fixes: [Turn on semantic search](./semantic-search.md).
- dpkms serves the same document at [`GET /api/v1/search/graph`](../../api/api-rest.md#get-searchgraph), with entities filtered by the caller's entitlements; the dpkms web UI doesn't show it yet.

## Common failure modes

### No browser opened

- ctxt opens a browser only when both stdout and stderr are a terminal. Piped or captured runs (scripts, agents, CI) just print the URL. Copy it from stderr.
- If opening fails, ctxt prints `Warning: could not open a browser (...); open the URL above.` and keeps serving.

### The page says "2D mode"

- Your browser has WebGL turned off or unavailable. Everything works in 2D; enable hardware acceleration to get the 3D view.

### The URL stopped working

- The viewer stops after `--graph-idle-timeout` without a request (10 minutes by default) or when you press Ctrl-C. Run the command again; you'll get a new URL. To keep a graph around, save it with `-o graph.html`.

### The graph shows only the query

- The search found no candidates (`metadata.counts.candidates` is `0`). Try broader words, or drop filters such as `--source-type` or `--since`.

### Every candidate says `legs fts`

- `mode` is `fts_only`: there is no default embedding model. Register one and make it the default: [Turn on semantic search](./semantic-search.md).
- `mode` is `fts_fallback`: the vector search couldn't run; `metadata.semantic_status` and `metadata.vector_error` say why. `low_coverage` means the default model has no vectors yet: run `ctxt embeddings migrate --to <model_id>`.
- `mode` is `hybrid` but `counts.vector_hits` is `0`: no object with a vector under the default model passed your filters.

### A link I created is missing

- The graph only contains the search's candidates and the entities they mention. A link to an object that wasn't a candidate isn't drawn. Search for words that match both objects, or raise `--limit`.

### The graph is partial (`truncated`)

- A size cap was reached. Raise `--graph-max-nodes` or `--graph-max-edges`, or narrow the search.

### No `similar` edges

- `similar` edges need `--graph-similar` and stored vectors on both objects under the default embedding model (`metadata.vector_model`); with no default model there are none. For objects with several chunks, the closest pair of chunks counts. Edges appear only above `--graph-similar-threshold`; try a lower threshold, such as `0.6`.

## Related references

- [`search-retrieval.md`](./search-retrieval.md) — the search workflow this page builds on
- [`semantic-search.md`](./semantic-search.md) — turn on the vector search the graph's `vector` leg and `similar` edges need
- [`../reference/query-language-and-ranking.md`](../reference/query-language-and-ranking.md) — how ranking works
- [`../reference/api-cli-reference.md`](../reference/api-cli-reference.md) — command map
- `ctxt find "<query>" --explain` prints a per-signal score breakdown as text, for the returned results only

## Story alignment

- `US-0021` (search with result explanation)
