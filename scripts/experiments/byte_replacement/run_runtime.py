"""Compare requests captured from Agent.Run, including the Go replacement path."""
import argparse
import getpass
import json
from pathlib import Path
import random
import subprocess

from run import ROOT, REQUEST_OPTIONS, make_case, request


def capture(cases):
    proc = subprocess.run(
        ["go", "run", "./scripts/experiments/byte_replacement/runtime"],
        cwd=ROOT,
        input=json.dumps({"contents": [case[0] for case in cases],
                          "questions": [case[1] for case in cases]}),
        text=True, capture_output=True, check=True,
    )
    captured = json.loads(proc.stdout)
    if len(captured) != len(cases):
        raise RuntimeError("agent request count differs from case count")
    for case, item in zip(cases, captured):
        arms = item["arms"]
        if set(arms) != {"A", "B", "D"} or [len(arms[arm]) for arm in ("A", "B", "D")] != [3, 4, 4]:
            raise RuntimeError("unexpected agent request shape")
        if len({arms[arm][0]["content"] for arm in arms}) != 1 or len({arms[arm][-1]["content"] for arm in arms}) != 1:
            raise RuntimeError("system or current question differs across arms")
        source = case[0].replace("controlled-v1", item["revision"])
        if arms["A"][1]["content"] != source or arms["B"][1]["content"] != source:
            raise RuntimeError("source context changed in original/injection arm")
        if ("[semantix-reuse]" not in arms["B"][2]["content"] or
                "content=earlier managed context user message" not in arms["D"][2]["content"] or
                "release_channel" in arms["D"][2]["content"] or
                arms["B"][2]["content"].splitlines()[:3] != arms["D"][2]["content"].splitlines()[:3] or
                len(arms["D"][2]["content"]) >= len(arms["B"][2]["content"])):
            raise RuntimeError("runtime injection/replacement did not differ as expected")
        if len(arms["D"][1]["content"]) >= len(source):
            raise RuntimeError("managed context was not compacted")
    return captured


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cases", type=int, default=3)
    parser.add_argument("--seed", type=int, default=809)
    parser.add_argument("--live", action="store_true")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not 1 <= args.cases <= 12:
        parser.error("--cases must be from 1 to 12")
    cases = [make_case(i) for i in range(args.cases)]
    captured = capture(cases)
    key = getpass.getpass("GLM API key: ") if args.live else ""
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("w", encoding="utf-8", newline="\n") as report:
        for i, ((_, question, expected), item) in enumerate(zip(cases, captured)):
            expected = dict(expected)
            if "repository_revision" in expected:
                expected["repository_revision"] = item["revision"]
            order = list(item["arms"])
            random.Random(args.seed + i).shuffle(order)
            for arm in order:
                messages = item["arms"][arm]
                row = {"case": i, "arm": arm, "order": order, "expected": expected,
                       "request": {**REQUEST_OPTIONS, "messages": messages}}
                if args.live:
                    status, payload, duration = request(messages, key)
                    row.update({"http_status": status, "elapsed_seconds": duration,
                                "usage": payload.get("usage"),
                                "answer": payload.get("choices", [{}])[0].get("message", {}).get("content"),
                                "finish_reason": payload.get("choices", [{}])[0].get("finish_reason"),
                                "error": payload.get("error")})
                    try:
                        answer = (row["answer"] or "").strip()
                        if answer.startswith("```"):
                            answer = answer.split("\n", 1)[1].rsplit("```", 1)[0].strip()
                        row["correct"] = json.loads(answer) == expected
                    except (TypeError, ValueError):
                        row["correct"] = False
                report.write(json.dumps(row, ensure_ascii=False) + "\n")
                report.flush()
                print(f'case={i} arm={arm} status={row.get("http_status", "dry")} '
                      f'correct={row.get("correct", "unchecked")} '
                      f'prompt={((row.get("usage") or {}).get("prompt_tokens"))}')
                if args.live and status != 200:
                    raise RuntimeError(f"provider returned HTTP {status}; partial report retained")


if __name__ == "__main__":
    main()
