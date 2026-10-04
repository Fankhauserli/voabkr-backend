#!/usr/bin/env bash
set -euo pipefail

command -v protoc >/dev/null 2>&1 || { echo "Error: protoc is required" >&2; exit 1; }
command -v protoc-gen-go >/dev/null 2>&1 || { echo "Error: protoc-gen-go is required" >&2; exit 1; }
command -v protoc-gen-go-json >/dev/null 2>&1 || { echo "Error: protoc-gen-go-json is required" >&2; exit 1; }
command -v protoc-go-inject-tag >/dev/null 2>&1 || { echo "Error: protoc-go-inject-tag is required" >&2; exit 1; }

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "${ROOT_DIR}"

protoc -I=proto \
  --go_out=. --go_opt=module=github.com/Fankhauserli/voabkr-backend \
  --go-json_out=emit_defaults=true,allow_unknown=true:. --go-json_opt=module=github.com/Fankhauserli/voabkr-backend \
  proto/*.proto

for f in types/*.pb.go; do
  protoc-go-inject-tag -input="$f"
done
