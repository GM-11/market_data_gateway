#!/usr/bin/env bash
# Generates Go code for the shared protos into market_data_gateway/gen/exchange/v1/.
# Same as engine_events_bridge/gen.sh; only GO_PKG differs.
# Needs: protoc, protoc-gen-go, protoc-gen-go-grpc on PATH
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
set -euo pipefail
cd "$(dirname "$0")"

PROTO_ROOT=../proto
OUT=gen   # source_relative puts files at gen/exchange/v1/, matching GO_PKG
GO_PKG=market_data_gateway/gen/exchange/v1
FILES=(commands.proto engine_feed.proto) # engine_feed imports commands

M=()
for f in "${FILES[@]}"; do
  M+=("--go_opt=Mexchange/v1/$f=$GO_PKG" "--go-grpc_opt=Mexchange/v1/$f=$GO_PKG")
done

mkdir -p "$OUT"
protoc -I "$PROTO_ROOT" \
  --go_out="$OUT" --go_opt=paths=source_relative \
  --go-grpc_out="$OUT" --go-grpc_opt=paths=source_relative \
  "${M[@]}" \
  $(printf "exchange/v1/%s " "${FILES[@]}")
echo "generated into $OUT/exchange/v1"
