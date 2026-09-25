#!/usr/bin/env bash
# 初始化 retrieval-lab 双臂实验环境（docs/specs/local-retrieval-model.md §3）。
# 用法：setup-lab.sh [lab-dir]   默认 ~/.semantix-lab/retrieval-lab
# 幂等：已存在的文件不覆盖（gateway.toml 除外，--force 才覆盖）。
set -euo pipefail

LAB="${1:-$HOME/.semantix-lab/retrieval-lab}"
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FORCE="${FORCE:-0}"

mkdir -p "$LAB"/{on,off}/sessions "$LAB"/train/{datasets,checkpoints,staging,reports}

for ARM in on off; do
  dst="$LAB/$ARM/gateway.toml"
  if [ ! -f "$dst" ] || [ "$FORCE" = "1" ]; then
    sed "s|__LAB__|$LAB|g" "$SRC/gateway-$ARM.toml" > "$dst"
    echo "wrote $dst"
  fi
done

for f in run-arm.sh trainctl.sh; do
  if [ ! -f "$LAB/$f" ] || [ "$FORCE" = "1" ]; then
    cp "$SRC/$f" "$LAB/$f" && chmod +x "$LAB/$f"
    echo "wrote $LAB/$f"
  fi
done

if [ ! -f "$LAB/lab.env" ]; then
  umask 177
  printf 'SEMANTIX_GATEWAY_KEY=lab-%s\n' "$(head -c16 /dev/urandom | xxd -p)" > "$LAB/lab.env"
  umask 022
  echo "wrote $LAB/lab.env (0600)"
fi

cat <<EOF

retrieval-lab ready at $LAB
next steps:
  1. 构建 lab 二进制:   go build -o ~/.semantix-lab/bin/semantix ./cmd/semantix
                        go build -o ~/.semantix-lab/bin/semantix-gateway ./cmd/semantix-gateway
  2. 启动双臂:          $LAB/run-arm.sh on   |   $LAB/run-arm.sh off
  3. 冷启动+首轮训练:   $LAB/trainctl.sh bootstrap
  4. 起重排 sidecar:    $LAB/trainctl.sh serve
  5. 自进化守护:        $LAB/trainctl.sh watch
EOF
