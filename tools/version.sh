#!/bin/sh
# version.sh - the lapcam version of this checkout, from git (the only source):
#   tools/version.sh            -> v2.0.0            (on the tag)
#                                  v2.0.0-3-g1a2b3c4  (3 commits after it)
#                                  v2.0.0-dirty       (uncommitted changes)
#                                  g1a2b3c4           (no tag reachable)
# Every part of a release (firmware image, lapcam, osd-feed, beacon, the SD
# card) carries this value: the build scripts call it (docs/release.md).
set -e
cd "$(dirname "$0")/.."
v=$(git describe --tags --match 'v*' --abbrev=7 --dirty --always 2>/dev/null) || v=unknown
case "$v" in
v*) ;;
unknown) ;;
*) v="g$v" ;; # a bare hash: no tag in sight
esac
printf '%s\n' "$v"
