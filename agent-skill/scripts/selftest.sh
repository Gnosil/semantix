#!/usr/bin/env bash
# selftest.sh — closed-loop self-test for the semantix middleware install.
# Exercises the full memory loop: extract -> lookup (zone=hit) -> inject.
# Exit 0 with "SELFTEST PASS" on success; non-zero otherwise.
#
# Usage: bash <(curl -sL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/selftest.sh)
set -euo pipefail

SEMANTIX="${SEMANTIX_BIN:-semantix}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() { echo "SELFTEST FAIL: $1" >&2; exit 1; }

"$SEMANTIX" help >/dev/null 2>&1 || fail "semantix binary not found (run install.sh first)"

# --- session A: seed a preference slice ---
cat > "$TMP/sA.jsonl" <<'EOF'
{"role":"user","content":"我办案喜欢先用模板，再补细节"}
{"role":"assistant","content":"好的，已记录"}
{"role":"user","content":"处理案件时我一般先套模板，再补细节"}
{"role":"assistant","content":"明白"}
EOF
"$SEMANTIX" extract --input "$TMP/sA.jsonl" --scope user --project selftest --db "$TMP/mem.db" >/dev/null \
  || fail "extract failed"

# --- lookup must find a hit; grey is not eligible for default injection ---
LOOKUP="$("$SEMANTIX" lookup --query "办案偏好" --scope user --db "$TMP/mem.db" --json 2>/dev/null || true)"
echo "$LOOKUP" | grep -q '"zone": "hit"' || fail "lookup returned no hit zone: $LOOKUP"

# --- inject must return a non-empty [semantix-reuse] block ---
INJECT="$("$SEMANTIX" inject --query "处理案件" --scope user --db "$TMP/mem.db" 2>/dev/null || true)"
echo "$INJECT" | grep -q '\[semantix-reuse\]' || fail "inject returned no reuse block"

echo "SELFTEST PASS: extract → lookup(hit) → inject closed loop verified"
