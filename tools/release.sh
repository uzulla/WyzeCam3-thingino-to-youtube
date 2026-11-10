#!/bin/sh
# release.sh - package a lapcam release (docs/release.md). Run on the PC:
#   tools/release.sh v2.0.0
#
# Expects the tag to exist on HEAD (git tag -a v2.0.0 -m ...), the tree to be
# clean, and dist/ to hold everything built from it:
#   dist/firmware/thingino-<camera>.bin + VERSION + BUILD   (firmware/build.sh, on the build host)
#   dist/lapcam, dist/osd-feed, dist/beacon (+ .version)     (device/*/build.sh)
#   dist/ffmpeg                                              (firmware/build.sh)
# Every version file must say the tag. Then it makes
#   dist/release/lapcam-<tag>-sdcard/        a stock-camera card (installer + image + programs + settings)
#   dist/release/lapcam-<tag>-sdcard.zip     the same, to unzip onto a FAT32 card
#   dist/release/lapcam-<tag>-firmware.bin   the image alone (for autoupdate-full.bin / sysupgrade)
#   dist/release/SHA256SUMS
# and prints the gh command that uploads them to GitHub Releases (not run).

set -e

TAG=$1
CAMERA=${CAMERA:-wyze_cam3_t31x_gc2053_atbm6031}
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/.." && pwd)

case "$TAG" in
v[0-9]*.[0-9]*.[0-9]*) ;;
*) echo "Usage: $0 vX.Y.Z" >&2; exit 1 ;;
esac
cd "$REPO"
if [ -n "$(git status --porcelain)" ]; then
	echo "the tree has uncommitted changes; commit or stash them first" >&2
	exit 1
fi
if [ "$(git describe --tags --match 'v*' --exact-match 2>/dev/null)" != "$TAG" ]; then
	echo "HEAD is not tagged $TAG (git tag -a $TAG -m '...'; tools/version.sh says $(sh tools/version.sh))" >&2
	exit 1
fi

echo "Checking dist/ for $TAG"
bad=0
for v in firmware/VERSION lapcam.version osd-feed.version beacon.version; do
	got=$(cat "dist/$v" 2>/dev/null || echo missing)
	if [ "$got" != "$TAG" ]; then
		echo "  dist/$v: $got (rebuild from the tag)" >&2
		bad=1
	else
		echo "  dist/$v: $got"
	fi
done
[ -f dist/ffmpeg ] || { echo "  dist/ffmpeg missing" >&2; bad=1; }
[ $bad = 0 ] || exit 1

for c in zip shasum; do
	command -v "$c" >/dev/null 2>&1 || { echo "$c is needed" >&2; exit 1; }
done
OUT=dist/release
rm -rf "$OUT/lapcam-$TAG-sdcard" "$OUT/lapcam-$TAG-sdcard.zip" "$OUT/lapcam-$TAG-firmware.bin"
mkdir -p "$OUT/lapcam-$TAG-sdcard"
echo "Making the card"
sh firmware/make-sdcard.sh "$OUT/lapcam-$TAG-sdcard" # (not piped: its exit status must stop this script)
cp "dist/firmware/thingino-$CAMERA.bin" "$OUT/lapcam-$TAG-firmware.bin"
(cd "$OUT" && rm -f SHA256SUMS && zip -q -r "lapcam-$TAG-sdcard.zip" "lapcam-$TAG-sdcard" && shasum -a 256 "lapcam-$TAG-sdcard.zip" "lapcam-$TAG-firmware.bin" >SHA256SUMS)
echo
cat "$OUT/SHA256SUMS"
echo
echo "To publish (after writing the notes in dist/release/notes-$TAG.md):"
echo "  gh release create $TAG --title \"$TAG\" --notes-file dist/release/notes-$TAG.md \\"
echo "    $OUT/lapcam-$TAG-sdcard.zip $OUT/lapcam-$TAG-firmware.bin $OUT/SHA256SUMS dist/lapcam dist/osd-feed dist/beacon dist/ffmpeg"
