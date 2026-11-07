#!/bin/sh
set -eu
PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
command -v go >/dev/null 2>&1 || { echo "需要安装 Go 1.25+"; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "需要安装 Node.js 22+ 与 npm"; exit 1; }
cd "$PROJECT_ROOT/web"
if [ ! -d node_modules ]; then npm ci; fi
npm run build
mkdir -p "$PROJECT_ROOT/.cache"
cd "$PROJECT_ROOT/server"
CGO_ENABLED=0 go build -o "$PROJECT_ROOT/.cache/lypit-studio" .
exec "$PROJECT_ROOT/.cache/lypit-studio" -root "$PROJECT_ROOT" "$@"
