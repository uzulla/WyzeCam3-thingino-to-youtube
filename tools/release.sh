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

# vX.Y.Z, optionally -rc.N; nothing else (the tag ends up in paths and rm -rf)
if ! expr "$TAG" : '^v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\(-rc\.[0-9][0-9]*\)\{0,1\}$' >/dev/null; then
	echo "Usage: $0 vX.Y.Z (or vX.Y.Z-rc.N)" >&2
	exit 1
fi
cd "$REPO"
if [ -n "$(git status --porcelain)" ]; then
	echo "the tree has uncommitted changes; commit or stash them first" >&2
	exit 1
fi
# the tag must be on HEAD (lightweight or annotated) and version.sh - what
# every build embeds - must say exactly that
if ! git tag --points-at HEAD --list "$TAG" | grep -qx "$TAG"; then
	echo "HEAD is not tagged $TAG (git tag -a $TAG -m '...'; tools/version.sh says $(sh tools/version.sh))" >&2
	exit 1
fi
if [ "$(sh tools/version.sh)" != "$TAG" ]; then
	echo "tools/version.sh says $(sh tools/version.sh), not $TAG (another tag on HEAD, or a dirty tree)" >&2
	exit 1
fi

echo "Checking dist/ for $TAG"
bad=0
for v in firmware/VERSION lapcam.version osd-feed.version beacon.version ffmpeg.version; do
	got=$(cat "dist/$v" 2>/dev/null || echo missing)
	if [ "$got" != "$TAG" ]; then
		echo "  dist/$v: $got (rebuild from the tag)" >&2
		bad=1
	else
		echo "  dist/$v: $got"
	fi
done
# the binaries themselves carry the version string (-ldflags -X): a .version
# file next to a binary copied in by hand would otherwise pass
for b in lapcam osd-feed beacon; do
	if [ ! -f "dist/$b" ]; then
		echo "  dist/$b missing" >&2
		bad=1
	elif ! grep -aqF "$TAG" "dist/$b"; then
		echo "  dist/$b does not contain the string $TAG (built from another commit?)" >&2
		bad=1
	fi
done
[ -f dist/ffmpeg ] || { echo "  dist/ffmpeg missing" >&2; bad=1; }
[ -f "dist/firmware/thingino-$CAMERA.bin" ] || { echo "  dist/firmware/thingino-$CAMERA.bin missing" >&2; bad=1; }
[ $bad = 0 ] || exit 1

for c in zip shasum; do
	command -v "$c" >/dev/null 2>&1 || { echo "$c is needed" >&2; exit 1; }
done
OUT=dist/release
# everything is made in a work directory and moved into place at the end:
# a failure leaves the previous release untouched and nothing half-made
WORK=$OUT/.work-$TAG
rm -rf "$WORK"
mkdir -p "$WORK/lapcam-$TAG-sdcard"
trap 'rm -rf "$WORK"' EXIT
echo "Making the card"
sh firmware/make-sdcard.sh -c "$CAMERA" "$WORK/lapcam-$TAG-sdcard" # (not piped: its exit status must stop this script)
cp "dist/firmware/thingino-$CAMERA.bin" "$WORK/lapcam-$TAG-firmware.bin"
(cd "$WORK" && zip -q -r "lapcam-$TAG-sdcard.zip" "lapcam-$TAG-sdcard" && shasum -a 256 "lapcam-$TAG-sdcard.zip" "lapcam-$TAG-firmware.bin" >SHA256SUMS)
# the single binaries go up too: list them under their upload names
(cd dist && shasum -a 256 lapcam osd-feed beacon ffmpeg) >>"$WORK/SHA256SUMS"
rm -rf "$OUT/lapcam-$TAG-sdcard" "$OUT/lapcam-$TAG-sdcard.zip" "$OUT/lapcam-$TAG-firmware.bin" "$OUT/SHA256SUMS"
mv "$WORK/lapcam-$TAG-sdcard" "$WORK/lapcam-$TAG-sdcard.zip" "$WORK/lapcam-$TAG-firmware.bin" "$WORK/SHA256SUMS" "$OUT/"
echo
cat "$OUT/SHA256SUMS"
echo
echo "To publish (after writing the notes in dist/release/notes-$TAG.md):"
echo "  gh release create $TAG --title \"$TAG\" --notes-file dist/release/notes-$TAG.md \\"
echo "    $OUT/lapcam-$TAG-sdcard.zip $OUT/lapcam-$TAG-firmware.bin $OUT/SHA256SUMS dist/lapcam dist/osd-feed dist/beacon dist/ffmpeg"
