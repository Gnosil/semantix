#!/usr/bin/env python3
"""Two-arm evidence report (spec M6): aggregate on/off usage, retrieval
events and store acceptance into one markdown comparison.

  uv run python compare_arms.py --lab ~/.semantix-lab/retrieval-lab \
      [--report train/reports/arms.md]
"""

import argparse
import datetime
from pathlib import Path

from semantix_ml.compare import arm_summary, render_comparison


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--lab", required=True)
    ap.add_argument("--report", default=None)
    args = ap.parse_args()

    lab = Path(args.lab).expanduser()
    text = (
        f"# retrieval-lab 双臂对照 — {datetime.date.today()}\n\n"
        + render_comparison(arm_summary(lab / "on"), arm_summary(lab / "off"))
        + "\nacceptance = store-level Injected/(Injected+Rejected)；"
        "admitted/candidates 来自检索事件日志。\n"
    )
    print(text)
    if args.report:
        out = Path(args.report).expanduser()
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(text)
        print(f"-> {out}")


if __name__ == "__main__":
    main()
