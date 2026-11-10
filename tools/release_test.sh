#!/bin/sh
# release_test.sh - tools/release.sh against a throw-away repository with a
# tag and a fake dist/ (no network, no build, no camera):
#   sh tools/release_test.sh
# Checks: refuses a dirty tree, a HEAD without the tag, a bad tag name, a
# .version that does not say the tag, a binary without the tag string, a
# missing part; makes the release when all is in place; a failure half way
# leaves the previous release untouched.

set -e
HERE=$(cd "$(dirname "$0")" && pwd)
REPO_SRC=$(cd "$HERE/.." && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
fail() {
	echo "FAIL: $*" >&2
	exit 1
}
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
R=$T/repo
mkdir -p "$R/tools" "$R/firmware/sdcard" "$R/dist/firmware" "$R/dist/installer"
cp "$HERE/release.sh" "$HERE/version.sh" "$R/tools/"
cp "$REPO_SRC/firmware/make-sdcard.sh" "$REPO_SRC/firmware/fatextract.py" "$R/firmware/"
cp "$REPO_SRC/firmware/sdcard/"* "$R/firmware/sdcard/"
cd "$R"
printf 'dist/\n' >.gitignore
git init -q && git add -A && git commit -q -m one
TAG=v9.9.9
CAM=wyze_cam3_t31x_gc2053_atbm6031

# a fake dist/ as the build scripts would leave it
fake_dist() {
	head -c 16777216 /dev/zero >"dist/firmware/thingino-$CAM.bin"
	echo "$TAG" >dist/firmware/VERSION
	echo "ciao+da40db6, test" >dist/firmware/BUILD
	for b in lapcam osd-feed beacon; do printf 'binary %s\n' "$TAG" >"dist/$b"; echo "$TAG" >"dist/$b.version"; done
	printf 'ffmpeg\n' >dist/ffmpeg
	echo "$TAG" >dist/ffmpeg.version
	echo installer >dist/installer/factory_t31_ZMC6tiIDQN
	echo test >dist/installer/SOURCE
}
fake_dist

echo "1. no tag on HEAD"
if sh tools/release.sh $TAG >/dev/null 2>&1; then fail "ran without the tag"; fi
git tag -a $TAG -m $TAG

echo "2. bad tag names"
for bad in 9.9.9 v9.9 "v9.9.9/../x" v9.9.9-beta; do
	if sh tools/release.sh "$bad" >/dev/null 2>&1; then fail "accepted tag '$bad'"; fi
done

echo "3. dirty tree"
echo x >scratch.txt
if sh tools/release.sh $TAG >/dev/null 2>&1; then fail "ran with an untracked file"; fi
rm scratch.txt

echo "4. a .version that does not say the tag"
echo v9.9.8 >dist/lapcam.version
if sh tools/release.sh $TAG >/dev/null 2>&1; then fail "ran with a wrong .version"; fi
echo "$TAG" >dist/lapcam.version

echo "5. a binary without the tag string"
printf 'binary v9.9.8\n' >dist/beacon
if sh tools/release.sh $TAG >/dev/null 2>&1; then fail "ran with a binary from another build"; fi
printf 'binary %s\n' "$TAG" >dist/beacon

echo "6. a missing part"
mv dist/ffmpeg dist/ffmpeg.away
if sh tools/release.sh $TAG >/dev/null 2>&1; then fail "ran without ffmpeg"; fi
mv dist/ffmpeg.away dist/ffmpeg

echo "7. the release"
out=$(sh tools/release.sh $TAG 2>&1) || fail "release failed: $out"
for f in "lapcam-$TAG-sdcard/thingino-$CAM.bin" "lapcam-$TAG-sdcard/factory_t31_ZMC6tiIDQN" "lapcam-$TAG-sdcard/VERSION.txt" "lapcam-$TAG-sdcard.zip" "lapcam-$TAG-firmware.bin" SHA256SUMS; do
	[ -f "dist/release/$f" ] || fail "missing dist/release/$f"
done
[ "$(grep -c . dist/release/SHA256SUMS)" = 6 ] || fail "SHA256SUMS lines: $(cat dist/release/SHA256SUMS)"
printf '%s\n' "$out" | grep -q "gh release create $TAG" || fail "no gh command shown"
[ ! -d "dist/release/.work-$TAG" ] || fail "work directory left behind"
sum=$(shasum -a 256 "dist/release/lapcam-$TAG-sdcard.zip" | cut -d' ' -f1)

echo "8. a failure half way keeps the previous release"
chmod 000 dist/osd-feed
if sh tools/release.sh $TAG >/dev/null 2>&1; then chmod 644 dist/osd-feed; fail "release succeeded with an unreadable binary"; fi
chmod 644 dist/osd-feed
[ "$(shasum -a 256 "dist/release/lapcam-$TAG-sdcard.zip" | cut -d' ' -f1)" = "$sum" ] || fail "previous zip was replaced"
[ -f dist/release/SHA256SUMS ] || fail "previous SHA256SUMS lost"
[ ! -d "dist/release/.work-$TAG" ] || fail "work directory left behind after a failure"

echo "all tests passed"
