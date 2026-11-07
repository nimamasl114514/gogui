#!/usr/bin/env bash
# gogui slim 构建（Git Bash / Linux / macOS）：-trimpath -s -w
set -euo pipefail
OUT="${1:-bin}"
mkdir -p "$OUT"
for app in demo monitor gogui-golden; do
  go build -trimpath -ldflags "-s -w" -o "$OUT/$app.exe" "./cmd/$app"
  printf "built %-26s %8s KB\n" "$OUT/$app.exe" "$(( $(stat -c%s "$OUT/$app.exe") / 1024 ))"
done
