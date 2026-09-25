#!/usr/bin/env bash
# retrieval-lab 训练控制入口（spec §3/§6）。
#   trainctl.sh bootstrap   合成 query 冷启动 + 首轮训练（需 SEMANTIX_SYNTH_* env）
#   trainctl.sh train       手动跑一轮：数据集→训练→ONNX→门禁→发布
#   trainctl.sh watch       自进化守护（攒批去抖 + 门禁 + 热载）
#   trainctl.sh serve       起重排 sidecar（127.0.0.1:8689，SIGHUP 热载）
#   trainctl.sh eval        对当前发布版跑一次 held-out 评估
#   trainctl.sh rollback [vNNNN]  回滚到上一版/指定版并热载
#   trainctl.sh status      当前版本、checkpoint 列表、数据量
set -euo pipefail

LAB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# ML 源码目录：优先环境变量，否则按仓库常规位置探测。
ML="${SEMANTIX_ML_DIR:-$HOME/semantix/semantix/scripts/ml}"
BIN="${SEMANTIX_BIN:-$HOME/.semantix-lab/bin/semantix}"
CMD="${1:-status}"
shift || true

cd "$ML"

latest_dataset() { ls -1dt "$LAB"/train/datasets/*/ 2>/dev/null | head -1; }

case "$CMD" in
bootstrap)
  EXPORT="$LAB/train/datasets/bootstrap-export.jsonl"
  "$BIN" export --db "$LAB/on/gateway.jsonl" --out "$EXPORT"
  uv run python synth_queries.py --slices "$EXPORT" --out "$LAB/train/datasets/synth.jsonl" "$@"
  exec "$LAB/trainctl.sh" train
  ;;
train)
  exec uv run python watch_and_train.py --lab "$LAB" --semantix-bin "$BIN" --once "$@"
  ;;
watch)
  exec uv run python watch_and_train.py --lab "$LAB" --semantix-bin "$BIN" "$@"
  ;;
serve)
  echo $$ > "$LAB/rerank-server.pid"
  exec uv run --extra serve python rerank_server.py --model-dir "$LAB/train/current" "$@"
  ;;
eval)
  DS="$(latest_dataset)"
  [ -n "$DS" ] || { echo "trainctl: no dataset yet — run train first" >&2; exit 1; }
  exec uv run --extra serve python eval_retrieval.py --data "$DS/heldout.jsonl" \
    --model "$LAB/train/current" "$@"
  ;;
rollback)
  V="$(uv run python -c "
import sys; sys.path.insert(0, '.')
from semantix_ml.registry import rollback
print(rollback('$LAB/train'${1:+, '$1'}))
")"
  echo "rolled back to $V"
  if [ -f "$LAB/rerank-server.pid" ]; then
    kill -HUP "$(cat "$LAB/rerank-server.pid")" 2>/dev/null && echo "rerank server reloaded" || true
  fi
  ;;
status)
  uv run python -c "
import json, pathlib, sys; sys.path.insert(0, '.')
from semantix_ml.registry import current_version, list_versions
lab = pathlib.Path('$LAB')
print('current: ', current_version(lab / 'train'))
print('versions:', list_versions(lab / 'train'))
ev = lab / 'on' / 'events.jsonl'
n = sum(1 for _ in open(ev)) if ev.exists() else 0
print('events:  ', n)
state = lab / 'train' / 'watch-state.json'
print('watch:   ', json.loads(state.read_text()) if state.exists() else '(never ran)')
"
  ;;
*)
  echo "usage: trainctl.sh bootstrap|train|watch|serve|eval|rollback|status" >&2
  exit 2
  ;;
esac
