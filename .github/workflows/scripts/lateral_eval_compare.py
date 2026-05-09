#!/usr/bin/env python3
"""Compare lateral eval metrics between PR and main baseline.

Reads JSON output from `ctxt lateral eval metrics --json` runs in
$OUT_DIR (pr-*.json + baseline-*.json), produces a markdown table to
$COMMENT_PATH, and exits 1 if any per-strategy precision or recall
dropped > 0.1 from baseline.

Sets ${GITHUB_OUTPUT}.regressed = "true|false" so the calling step can
gate on it.

Used by .github/workflows/lateral-eval.yml.
"""

import glob
import json
import os
import sys


def load_json(path: str) -> dict:
    try:
        with open(path) as fh:
            return json.load(fh)
    except (OSError, json.JSONDecodeError):
        return {"strategies": {}}


def main() -> int:
    out_dir = os.environ.get("OUT_DIR", "out")
    comment_path = os.environ.get("COMMENT_PATH", "eval-comment.md")
    regression_threshold = 0.1

    rows = []
    regressions: list[tuple[str, str, float, float]] = []

    for f in sorted(glob.glob(os.path.join(out_dir, "pr-*.json"))):
        name = os.path.basename(f).replace("pr-", "").replace(".json", "")
        base_f = os.path.join(out_dir, f"baseline-{name}.json")
        pr = load_json(f)
        base = load_json(base_f)
        for sid, m in pr.get("strategies", {}).items():
            bm = base.get("strategies", {}).get(sid, {})
            bp = bm.get("precision", 0.0)
            br = bm.get("recall", 0.0)
            dp = m.get("precision", 0.0) - bp
            dr = m.get("recall", 0.0) - br
            rows.append((name, sid, m.get("precision", 0.0), m.get("recall", 0.0), dp, dr))
            if dp < -regression_threshold or dr < -regression_threshold:
                regressions.append((name, sid, dp, dr))

    lines = ["## Lateral Eval — precision/recall vs main", ""]
    if not rows:
        lines.append("_No PR fixture metrics produced — check the eval step output._")
    else:
        lines.append("| Fixture | Strategy | Precision | Recall | Δ Precision | Δ Recall |")
        lines.append("|---|---|---|---|---|---|")
        for name, sid, p, r, dp, dr in rows:
            lines.append(f"| {name} | {sid} | {p:.2f} | {r:.2f} | {dp:+.2f} | {dr:+.2f} |")

    if regressions:
        lines.append("")
        lines.append("### REGRESSIONS (precision or recall dropped > 0.1)")
        for name, sid, dp, dr in regressions:
            lines.append(f"- {name}/{sid}: ΔP={dp:+.2f} ΔR={dr:+.2f}")

    with open(comment_path, "w") as fh:
        fh.write("\n".join(lines) + "\n")

    output_path = os.environ.get("GITHUB_OUTPUT")
    if output_path:
        with open(output_path, "a") as fh:
            fh.write(f"regressed={'true' if regressions else 'false'}\n")

    # Print the table for the workflow log.
    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main())
