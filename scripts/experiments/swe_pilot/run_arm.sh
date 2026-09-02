#!/bin/bash
# run_arm.sh — SWE-bench pilot runner (efficiency research plan W1).
#
# Drives semantix-agent non-interactively over the pilot instances for one
# arm, producing SWE-bench predictions + per-run metrics.
#
#   ON  arm: the repo's .semantix/project.db persists across instances and
#            every completed instance's session JSONL is extracted into it —
#            instance N starts with instance 0..N-1's slices (Project scope,
#            same-repo cross-instance transfer).
#   OFF arm: semantix disabled entirely; .semantix wiped before every run.
#
# Usage: run_arm.sh <on|off> <out_dir>
set -u
ARM="$1"; OUT="$2"
# Overridable so one script drives arms built from different revisions
# (BIN=/tmp/semantix-agent-old for the pre-distill baseline arm).
BIN="${BIN:-/tmp/semantix-agent}"
KBIN="${KBIN:-/tmp/semantix}"
PILOT="${PILOT_DIR:-/tmp/semantix-run/pilot}"
REPOS=/tmp/semantix-run/repos
MAX_STEPS="${MAX_STEPS:-40}"
mkdir -p "$OUT"

for inst_file in "$PILOT"/*.json; do
  id=$(python3.12 -c "import json;print(json.load(open('$inst_file'))['instance_id'])")
  repo=$(python3.12 -c "import json;print(json.load(open('$inst_file'))['repo'])")
  commit=$(python3.12 -c "import json;print(json.load(open('$inst_file'))['base_commit'])")
  short_repo=${repo#*/}
  echo "=== [$ARM] $id ($commit) ==="

  rd="$REPOS/$short_repo"
  cd "$rd" || exit 1
  git checkout -f "$commit" 2>&1 | tail -1
  git clean -fdx -q -e .semantix 2>/dev/null
  git log --oneline -1

  # Local excludes so runtime artifacts never enter the model diff.
  cat .git/info/exclude 2>/dev/null | grep -q "semantix-agent.toml" || printf "semantix-agent.toml\n.semantix/\n" >> .git/info/exclude

  # Arm-specific kernel wiring.
  if [ "$ARM" = "on" ]; then
    cat > semantix-agent.toml <<EOF
[semantix]
enabled = true
binary = "/tmp/semantix"
inject = true
budget = 4096
sessions_dir = "$OUT/sessions"
EOF
    mkdir -p "$OUT/sessions"
  else
    cat > semantix-agent.toml <<EOF
[semantix]
enabled = false
EOF
    rm -rf .semantix
  fi

  # Warm the ON arm: extract every previous session JSONL of this repo.
  if [ "$ARM" = "on" ]; then
    for prev in "$OUT/sessions"/*.jsonl; do
      [ -e "$prev" ] || continue
      pname=$(basename "$prev")
      case "$pname" in
        django_*) prev_repo=django ;;
        sympy_*)  prev_repo=sympy ;;
        *) continue ;;
      esac
      [ "$prev_repo" = "$short_repo" ] || continue
      # EXTRACT_FLAGS (e.g. "--distill --consolidate") selects the four-layer
      # supply side for the ON-new arm; empty keeps the legacy transcript path.
      $KBIN extract --input "$prev" --db "$rd/.semantix/project.db" --session "${pname%.jsonl}" ${EXTRACT_FLAGS:-} > /dev/null 2>&1
    done
  fi

  task=$(python3.12 - "$inst_file" <<'PYEOF'
import json, sys
d = json.load(open(sys.argv[1]))
print(f"""You are solving a real GitHub issue in this repository (your cwd is the repo root at the pre-fix commit).

Issue:
{d['problem_statement']}

Instructions:
- Locate the code responsible and implement the minimal, correct fix.
- Do NOT modify test files.
- Python dependencies for running the full test suite may not be installed in this environment; prefer careful code reasoning over executing the full suite. You may run quick syntax checks.
- Keep changes focused; no unrelated refactoring.
- When the fix is complete, stop.""")
PYEOF
)

  # Snapshot existing session mirrors so we can identify this run's file.
  # The bridge's session label is the model name (harness/boot: label :=
  # entry.Model), so every run appends to the SAME mirror file. Wipe it
  # before the run and move it after — each run's mirror is then exactly
  # the delta it produced.
  rm -f "$OUT/sessions"/*.jsonl 2>/dev/null
  ls "$OUT/sessions" 2>/dev/null | sort > "$OUT/.sessions.before"

  # Agent-side session store snapshot: num_turns in the result JSON is a
  # known dead counter (TurnDone never fires on this headless path — W1's
  # 0/1 column was pure fallback), so real turns are counted from the new
  # session JSONL the run writes under $SEMANTIX_HOME/projects/<slug>.
  AHOME="${SEMANTIX_HOME:-$HOME/.semantix-agent}/projects/$(echo "$rd" | tr '/' '-')/sessions"
  ls "$AHOME" 2>/dev/null | grep '\.jsonl$' | grep -v '\.events\.jsonl$' | sort > "$OUT/.ahome.before"

  start=$(date +%s)
  # Resume support: a completed run (parseable result JSON) is not redone —
  # the ON arm accumulates its library across instances, so a restart must
  # skip instead of re-running.
  if [ -s "$OUT/${id}.result.json" ] && python3.12 -c "import json,sys; json.load(open(sys.argv[1]))" "$OUT/${id}.result.json" 2>/dev/null; then
    echo "=== [$ARM] $id: already done, skipping ==="
    continue
  fi
  # Watchdog: a hung LLM connection previously stalled a run indefinitely.
  # A dedicated killer subshell fires kill -9 at the deadline — immune to
  # kill -0 race artifacts of a polling loop. rc 124 marks the timeout.
  $BIN -p --output-format json --max-steps "$MAX_STEPS" --permission-mode yolo "$task" > "$OUT/${id}.result.json" 2> "$OUT/${id}.stderr" &
  apid=$!
  limit="${AGENT_TIMEOUT_SECS:-1800}"
  ( sleep "$limit" && kill -9 "$apid" 2>/dev/null ) & killer=$!
  wait "$apid"
  rc=$?
  kill "$killer" 2>/dev/null
  if [ "$rc" = "137" ]; then rc=124; fi
  end=$(date +%s)
  echo "agent exit=$rc elapsed=$((end-start))s"

  # Model patch: tracked-file diff (runtime artifacts are locally excluded).
  git add -A -- . ':!.semantix' ':!semantix-agent.toml' 2>/dev/null
  git diff --cached HEAD --binary > "$OUT/${id}.patch" 2>/dev/null
  git reset -q
  # Tag this run's session mirror with repo+instance for the warm-up glob.
  for f in "$OUT/sessions"/*.jsonl; do
    [ -s "$f" ] || continue
    mv "$f" "$OUT/sessions/${short_repo}_${id}.jsonl"
  done

  # Real turn count: assistant lines in the session JSONL this run created.
  real_turns=""
  new_jsonl=$(ls "$AHOME" 2>/dev/null | grep '\.jsonl$' | grep -v '\.events\.jsonl$' | sort | comm -13 "$OUT/.ahome.before" - | tail -1)
  if [ -n "$new_jsonl" ]; then
    real_turns=$(python3.12 - "$AHOME/$new_jsonl" <<'PYEOF'
import json, sys
n = 0
for line in open(sys.argv[1]):
    line = line.strip()
    if not line:
        continue
    try:
        d = json.loads(line)
    except Exception:
        continue
    if d.get("role") == "assistant":
        n += 1
print(n)
PYEOF
)
  fi

  # Metrics summary: turns from the session JSONL (fallback ""), the rest
  # from the agent's JSON result.
  python3.12 - "$OUT/${id}.result.json" "$ARM" "$id" "$((end-start))" "$rc" "${real_turns:-}" <<'PYEOF' >> "$OUT/metrics.tsv"
import json, sys
try:
    d = json.load(open(sys.argv[1]))
    u = d.get("usage", {})
    turns = sys.argv[6] if len(sys.argv) > 6 and sys.argv[6] != "" else d.get("num_turns")
    print("\t".join(map(str, [sys.argv[3], sys.argv[2], sys.argv[4], sys.argv[5],
        turns, u.get("input_tokens"), u.get("output_tokens"),
        u.get("cache_read_input_tokens"), u.get("cache_creation_input_tokens"),
        d.get("total_cost_usd")])))
except Exception as e:
    print("\t".join(map(str, [sys.argv[3], sys.argv[2], sys.argv[4], sys.argv[5], "ERR", e])))
PYEOF
  echo "patch bytes: $(wc -c < "$OUT/${id}.patch")"
done
echo "[$ARM] done -> $OUT"
