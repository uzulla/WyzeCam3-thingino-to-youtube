#!/bin/sh
# version_test.sh - what tools/version.sh says in each state of a checkout:
#   sh tools/version_test.sh
# Builds a throw-away git repository (the real one is not touched), copies
# version.sh into it and checks: on a tag, commits after it, a changed tracked
# file, a new untracked file, no tag at all, and no git repository.

set -e
HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
fail() {
	echo "FAIL: $*" >&2
	exit 1
}
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
R=$T/repo
mkdir -p "$R/tools"
cp "$HERE/version.sh" "$R/tools/"
cd "$R"
git init -q
git add tools/version.sh
git commit -q -m one
git tag -a v2.0.0 -m v2.0.0

v=$(sh tools/version.sh)
[ "$v" = v2.0.0 ] || fail "on the tag: $v"

echo change >tracked.txt
git add tracked.txt
git commit -q -m two
h=$(git rev-parse --short=7 HEAD)
v=$(sh tools/version.sh)
[ "$v" = "v2.0.0-1-g$h" ] || fail "one commit after the tag: $v (want v2.0.0-1-g$h)"

echo more >>tracked.txt
v=$(sh tools/version.sh)
[ "$v" = "v2.0.0-1-g$h-dirty" ] || fail "changed tracked file: $v"
git checkout -q -- tracked.txt

echo new >untracked.go
v=$(sh tools/version.sh)
[ "$v" = "v2.0.0-1-g$h-dirty" ] || fail "untracked file: $v (must count as dirty)"
rm untracked.go

# a lightweight tag works too, and a non-v tag is ignored
git tag v2.0.1
v=$(sh tools/version.sh)
[ "$v" = v2.0.1 ] || fail "lightweight tag: $v"
git tag release-x
v=$(sh tools/version.sh)
[ "$v" = v2.0.1 ] || fail "non-v tag must be ignored: $v"

git tag -d v2.0.0 v2.0.1 release-x >/dev/null
v=$(sh tools/version.sh)
[ "$v" = "g$h" ] || fail "no tag: $v (want g$h)"

rm -rf .git
v=$(sh tools/version.sh)
[ "$v" = unknown ] || fail "no repository: $v"

echo "all tests passed"
