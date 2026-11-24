#!/usr/bin/env python3
"""Score complete human labels against Semantix verify JSON (offline)."""
import argparse
import hashlib
import json
import os
from collections import Counter
from pathlib import Path

ZONES = {"hit", "grey", "miss"}
LABELS_HEADER = "# semantix-verify-labels-v1 sha256="


def replay_rows(path):
    raw = path.read_bytes()
    envelope = json.loads(raw.decode("utf-8"))
    if envelope.get("ok") is not True or envelope.get("command") != "verify":
        raise ValueError("expected a successful semantix verify --json report")
    data = envelope["data"]
    rows = data["rows"] or []
    if not rows or len(rows) != data["replayed_turns"]:
        raise ValueError("empty or inconsistent replay")
    keyed = {}
    for index, row in enumerate(rows, 1):
        session, turn, zone = row["session"], row["turn"], row["zone"]
        if (not isinstance(session, str) or not session
                or any(c in session for c in "\t\r\n")
                or type(turn) is not int or turn < 1 or zone not in ZONES
                or not isinstance(row["top1_content"], str)):
            raise ValueError("invalid replay row identity or zone")
        key = (session, turn)
        if key in keyed:
            raise ValueError(f"duplicate replay row at position {index}")
        keyed[key] = row
    counts = Counter(row["zone"] for row in keyed.values())
    if any(counts[z] != data[f"zones_{z}"] for z in ZONES):
        raise ValueError("replay zones do not match summary")
    return keyed, counts, hashlib.sha256(raw).hexdigest()


def read_labels(path, keyed, replay_digest):
    prefix = LABELS_HEADER.encode("ascii")
    expected = prefix + replay_digest.encode("ascii")
    labels = {}
    with path.open("rb") as source:
        # Check the small ASCII header before decoding or parsing any marks.
        header = source.readline(len(expected) + 3).rstrip(b"\r\n")
        if not header.startswith(prefix):
            raise ValueError("labels have no supported replay binding; prepare a new TSV and relabel")
        if header != expected:
            raise ValueError("labels are bound to a different replay; use the original JSON or relabel")
        for num, raw_line in enumerate(source, 2):
            try:
                line = raw_line.decode("utf-8").rstrip("\r\n")
            except UnicodeDecodeError:
                raise ValueError(f"label line {num}: expected UTF-8") from None
            if not line or line.startswith("#"):
                continue
            parts = line.split("\t")
            if len(parts) != 3 or parts[2] not in ("0", "1"):
                raise ValueError(f"label line {num}: expected session<TAB>turn<TAB>0|1")
            try:
                key = (parts[0], int(parts[1]))
            except ValueError:
                raise ValueError(f"label line {num}: expected an integer turn") from None
            if key not in keyed or key in labels:
                raise ValueError(f"label line {num}: duplicate or unknown replay row")
            if parts[2] == "1" and not keyed[key]["top1_content"].strip():
                raise ValueError(f"label line {num}: no answer candidate cannot be relevant")
            labels[key] = int(parts[2])
    if len(labels) != len(keyed):
        raise ValueError(f"incomplete labels: {len(labels)} of {len(keyed)} rows")
    return labels


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("replay", type=Path)
    options = parser.add_mutually_exclusive_group(required=True)
    options.add_argument("--prepare", type=Path, help="create blank labels TSV")
    options.add_argument("--labels", type=Path, help="complete human labels TSV")
    args = parser.parse_args()
    try:
        keyed, zones, replay_digest = replay_rows(args.replay)
        if args.prepare:
            # No query or top-1 text in the template; view that locally in replay.
            with open(args.prepare, "x", encoding="utf-8", newline="\n",
                      opener=lambda path, flags: os.open(path, flags, 0o600)) as f:
                f.write(LABELS_HEADER + replay_digest + "\n")
                f.write("# session\tturn\trelevant_0_or_1\n")
                for session, turn in keyed:
                    f.write(f"{session}\t{turn}\t\n")
            return
        labels = read_labels(args.labels, keyed, replay_digest)
        n, relevant = len(keyed), sum(labels.values())
        by_zone = {z: sum(labels[k] for k in keyed if keyed[k]["zone"] == z)
                   for z in sorted(ZONES)}
        print(json.dumps({
            "replayed": n, "human_relevant": relevant,
            "human_yield_pct": round(100 * relevant / n, 3),
            "auto_hit_pct": round(100 * zones["hit"] / n, 3),
            "grey_pct": round(100 * zones["grey"] / n, 3),
            "zones": {z: zones[z] for z in sorted(ZONES)},
            "human_relevant_by_zone": by_zone,
            "human_yield_at_least_70_pct": relevant * 10 >= n * 7,
            "grey_at_most_30_pct": zones["grey"] * 10 <= n * 3,
        }, indent=2))
    except (OSError, AttributeError, KeyError, TypeError, ValueError) as e:
        parser.error(str(e))


if __name__ == "__main__":
    main()
