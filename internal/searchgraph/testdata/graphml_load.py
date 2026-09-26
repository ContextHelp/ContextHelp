"""Load a GraphML export with NetworkX (and python-igraph when installed).

Prints what survived as JSON on stdout, each attribute as a
[python type name, value] pair so the Go test can check typing.
Usage: python3 graphml_load.py FILE.graphml
"""

import json
import sys

import networkx as nx


def typed(attrs):
    return {k: [type(v).__name__, v] for k, v in attrs.items()}


def load_networkx(path):
    g = nx.read_graphml(path)
    out = {
        "directed": g.is_directed(),
        "graph": typed(g.graph),
        "nodes": {n: typed(d) for n, d in g.nodes(data=True)},
        "edges": [],
    }
    if g.is_multigraph():
        edges = ((u, v, k, d) for u, v, k, d in g.edges(keys=True, data=True))
    else:
        edges = ((u, v, d.get("id", ""), d) for u, v, d in g.edges(data=True))
    for u, v, k, d in edges:
        out["edges"].append({"source": u, "target": v, "id": str(k), "data": typed(d)})
    return out


def load_igraph(path):
    try:
        import igraph
    except ImportError:
        return None
    g = igraph.Graph.Read_GraphML(path)
    return {
        "nodes": g.vcount(),
        "edges": g.ecount(),
        "labels": g.vs["label"] if "label" in g.vs.attributes() else [],
        "relation": g.es["relation"] if "relation" in g.es.attributes() else [],
    }


def main():
    path = sys.argv[1]
    out = load_networkx(path)
    out["igraph"] = load_igraph(path)
    json.dump(out, sys.stdout, sort_keys=True)


if __name__ == "__main__":
    main()
