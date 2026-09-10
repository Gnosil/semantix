#!/usr/bin/env bash
# retrieval-lab 双臂网关启动器。用法：run-arm.sh on|off
# 凭证来源：QIANFAN_API_KEY 取自工作台主仓 backend/.env（不复制、不落盘）；
# SEMANTIX_GATEWAY_KEY 取自本目录 lab.env（本地实验密钥，0600）。
set -euo pipefail
ARM="${1:-}"
case "$ARM" in on|off) ;; *) echo "usage: $0 on|off" >&2; exit 2 ;; esac

LAB="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${SEMANTIX_GATEWAY_BIN:-$HOME/.semantix-lab/bin/semantix-gateway}"

set -a
source "$LAB/lab.env"
source "/Users/song/保画板/backend/.env"
set +a

exec "$BIN" -config "$LAB/$ARM/gateway.toml"
