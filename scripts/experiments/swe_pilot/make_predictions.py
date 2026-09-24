#!/usr/bin/env python3
"""Build a SWE-bench predictions.jsonl from a run_arm.sh output directory.

run_arm.sh writes one <instance_id>.patch per instance; the official harness
needs {"instance_id", "model_name_or_path", "model_patch"}.
"""
import json
import pathlib
import sys


def main() -> None:
    out_dir = pathlib.Path(sys.argv[1])
    label = sys.argv[2] if len(sys.argv) > 2 else out_dir.name
    rows = []
    for p in sorted(out_dir.glob("*.patch")):
        iid = p.name[: -len(".patch")]
        patch = p.read_text(errors="replace")
        if not patch.strip():
            continue
        rows.append({
            "instance_id": iid,
            "model_name_or_path": label,
            "model_patch": patch,
        })
    dest = out_dir / "predictions.jsonl"
    with dest.open("w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")
    print(f"{dest}: {len(rows)} predictions", file=sys.stderr)


if __name__ == "__main__":
    main()
