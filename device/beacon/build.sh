#!/bin/sh
# build.sh - cross-compile beacon for the camera (Wyze Cam v3: MIPS32 little
# endian, Linux 3.10, uClibc). Needs only Go on the PC: the binary is static.
#   device/beacon/build.sh            -> dist/beacon
# softfloat: the safe choice on this SoC; float math is not used in the loop anyway.
set -e
cd "$(dirname "$0")"
OUT=${1:-../../dist/beacon}
mkdir -p "$(dirname "$OUT")"
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags="-s -w" -o "$OUT" .
ls -l "$OUT"
