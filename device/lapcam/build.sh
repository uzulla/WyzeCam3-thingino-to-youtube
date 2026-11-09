#!/bin/sh
# build.sh - cross-compile lapcam for the camera (Wyze Cam v3: MIPS32 little
# endian, Linux 3.10, uClibc). Needs only Go on the PC: the binary is static.
#   device/lapcam/build.sh            -> dist/lapcam
set -e
OUT=${1:-$(cd "$(dirname "$0")/../.." && pwd)/dist/lapcam}
case "$OUT" in /*) ;; *) OUT=$(pwd)/$OUT ;; esac # keep a relative argument relative to the caller
cd "$(dirname "$0")"
mkdir -p "$(dirname "$OUT")"
go vet ./...
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags="-s -w" -o "$OUT" .
ls -l "$OUT"
