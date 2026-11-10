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
v=$(git describe --tags --match 'v[0-9]*' --abbrev=7 --dirty --always 2>/dev/null) || v=unknown
case "$v" in
v*) ;;
unknown) ;;
*) v="g$v" ;; # a bare hash: no tag in sight
esac
# --dirty sees changed tracked files only; a new source file that is not
# committed yet makes the build differ from the tag just as much
case "$v" in
*-dirty | unknown) ;;
*) [ -z "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ] || v="$v-dirty" ;;
esac
printf '%s\n' "$v"
