#!/bin/sh
# build.sh - cross-compile webui for the camera (Wyze Cam v3: MIPS32 little
# endian, Linux 3.10, uClibc). Needs only Go on the PC: the binary is static.
#   device/webui/build.sh            -> dist/webui
set -e
cd "$(dirname "$0")"
OUT=${1:-../../dist/webui}
mkdir -p "$(dirname "$OUT")"
go vet ./...
go test ./... >/dev/null
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags="-s -w" -o "$OUT" .
ls -l "$OUT"
