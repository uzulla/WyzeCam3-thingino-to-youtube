#!/bin/sh
# make-sdcard_test.sh - exercise firmware/make-sdcard.sh without a card, a
# network or a build: a throw-away copy of the repository with a fake dist/
# (and a fake installer cache), writing into temporary directories.
#   sh firmware/make-sdcard_test.sh
# Checks the option combinations (-u, -u -s, -w several, -k, -c), that
# settings on a card are kept, that a bad option changes nothing on the card,
# that no *.lapcam-tmp is left behind, and the flash markers. Exits 1 on the
# first failure.

set -e

HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
REPO=$T/repo
mkdir -p "$REPO/firmware/sdcard" "$REPO/dist/firmware" "$REPO/dist/installer"
cp "$HERE/make-sdcard.sh" "$HERE/fatextract.py" "$REPO/firmware/"
cp "$HERE/sdcard/"* "$REPO/firmware/sdcard/"
# a fake image of the right size, fake programs, a fake (cached) installer
head -c 16777216 /dev/zero >"$REPO/dist/firmware/thingino-wyze_cam3_t31x_gc2053_atbm6031.bin"
for p in lapcam osd-feed beacon ffmpeg; do echo "$p" >"$REPO/dist/$p"; done
echo installer >"$REPO/dist/installer/factory_t31_ZMC6tiIDQN"
echo test >"$REPO/dist/installer/SOURCE"
# the version files the build scripts leave next to their output
for v in firmware/VERSION lapcam.version osd-feed.version beacon.version ffmpeg.version; do echo v9.9.9 >"$REPO/dist/$v"; done
echo "ciao+da40db6, test" >"$REPO/dist/firmware/BUILD"
M=$REPO/firmware/make-sdcard.sh

fail() {
	echo "FAIL: $*" >&2
	exit 1
}
# card <name>: a fresh "card" directory (with a space in its path, like /Volumes/NO NAME)
card() {
	d="$T/cards/$1 card"
	mkdir -p "$d"
	echo "$d"
}
listing() { (cd "$1" && ls -A | LC_ALL=C sort | tr '\n' ' '); }
no_tmp() { [ -z "$(ls "$1" | grep 'lapcam-tmp$')" ] || fail "$2: *.lapcam-tmp left on the card"; }

echo "1. stock install card"
C=$(card stock)
sh "$M" "$C" >/dev/null
[ "$(listing "$C")" = "README.txt VERSION.txt beacon factory_t31_ZMC6tiIDQN ffmpeg lapcam lapcam.args osd-feed osd-feed.json prudynt-osd.json thingino-wyze_cam3_t31x_gc2053_atbm6031.bin " ] || fail "stock listing: $(listing "$C")"
[ "$(cat "$C/lapcam.args")" = "-listen :80" ] || fail "lapcam.args"
grep -q "^lapcam: v9.9.9$" "$C/VERSION.txt" && grep -q "^firmware .*: v9.9.9$" "$C/VERSION.txt" || fail "VERSION.txt: $(cat "$C/VERSION.txt")"
no_tmp "$C" stock

echo "1b. programs from another build than the firmware are reported"
echo v9.9.8 >"$REPO/dist/beacon.version"
output=$(sh "$M" "$C" 2>&1) || fail "mixed versions: making the card failed: $output"
printf '%s\n' "$output" | grep -q "different builds" || fail "mixed versions not reported"
grep -q "^beacon: v9.9.8$" "$C/VERSION.txt" || fail "VERSION.txt does not list the odd one: $(cat "$C/VERSION.txt")"
echo v9.9.9 >"$REPO/dist/beacon.version"
echo v9.9.7 >"$REPO/dist/firmware/VERSION" # the odd one first
output=$(sh "$M" "$C" 2>&1) || fail "mixed firmware: making the card failed"
printf '%s\n' "$output" | grep -q "different builds" || fail "mixed firmware version not reported"
echo v9.9.9 >"$REPO/dist/firmware/VERSION"
echo "1c. a part without a version file is reported as unknown, not as another build"
rm "$REPO/dist/osd-feed.version"
output=$(sh "$M" "$C" 2>&1) || fail "unknown version: making the card failed"
printf '%s\n' "$output" | grep -q "no version file" || fail "unknown version not reported: $output"
grep -q "^osd-feed: unknown$" "$C/VERSION.txt" || fail "VERSION.txt unknown: $(cat "$C/VERSION.txt")"
echo v9.9.9 >"$REPO/dist/osd-feed.version"

echo "2. upgrade card (-u): no marker, no installer"
C=$(card upgrade)
cp "$HERE/sdcard/README.txt" "$C/" # a card made earlier (the script accepts it)...
printf '1' >"$C/autoupdate-full.done" # ...with the marker left from that use: must go
sh "$M" -u "$C" >/dev/null
[ -f "$C/autoupdate-full.bin" ] || fail "-u: no autoupdate-full.bin"
[ ! -f "$C/autoupdate-full.done" ] || fail "-u: .done still there"
[ ! -f "$C/factory_t31_ZMC6tiIDQN" ] || fail "-u: installer on the card"
no_tmp "$C" upgrade

echo "3. upgrade card without flashing (-u -s): marker present"
C=$(card skip)
sh "$M" -u -s "$C" >/dev/null
[ -f "$C/autoupdate-full.bin" ] && [ "$(cat "$C/autoupdate-full.done")" = 1 ] || fail "-u -s: marker"
no_tmp "$C" skip

echo "4. -s without -u is refused"
C=$(card s-alone)
if sh "$M" -s "$C" >/dev/null 2>&1; then fail "-s alone accepted"; fi
[ -z "$(ls -A "$C")" ] || fail "-s alone wrote something"

echo "5. -w twice and -k: files and their content"
C=$(card wifi)
sh "$M" -u -w "Home Net" "home-pass-1" -w "Pocket" "pocket-pass" -k "abcd-1234-efgh" "$C" >/dev/null
[ "$(grep -c '^network={' "$C/wpa_supplicant.conf")" = 2 ] || fail "two networks"
grep -q 'ssid="Home Net"' "$C/wpa_supplicant.conf" || fail "ssid with a space"
[ "$(sed -n 's/.*priority=\([0-9]*\).*/\1/p' "$C/wpa_supplicant.conf" | tr '\n' ' ')" = "10 9 " ] || fail "priorities"
grep -q '"stream_key": "abcd-1234-efgh"' "$C/youtube-relay.json" || fail "stream key"
no_tmp "$C" wifi

echo "5b. -a: an SSH public key file, and refusal of what is not one"
printf 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFZgtestkeyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA me@pc\r\n# a comment\n\n' >"$T/key.pub"
sh "$M" -u -a "$T/key.pub" "$C" >/dev/null
[ "$(grep -c '^ssh-ed25519' "$C/authorized_keys")" = 1 ] || fail "authorized_keys not written"
grep -q "$(printf '\r')" "$C/authorized_keys" && fail "CR left in authorized_keys"
printf -- '-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n' >"$T/private"
if sh "$M" -u -a "$T/private" "$C" >/dev/null 2>&1; then fail "accepted a private key file"; fi
[ "$(grep -c '^ssh-ed25519' "$C/authorized_keys")" = 1 ] || fail "authorized_keys changed by a refused -a"

echo "6. a second run keeps the settings and replaces the programs"
echo "my args" >"$C/lapcam.args"
echo '{"mine":1}' >"$C/prudynt-osd.json"
echo newer >"$REPO/dist/lapcam"
before=$(cat "$C/wpa_supplicant.conf" "$C/youtube-relay.json" | cksum)
sh "$M" -u "$C" >/dev/null
[ "$(cat "$C/lapcam.args")" = "my args" ] || fail "lapcam.args not kept"
[ "$(cat "$C/prudynt-osd.json")" = '{"mine":1}' ] || fail "prudynt-osd.json not kept"
[ "$(cat "$C/wpa_supplicant.conf" "$C/youtube-relay.json" | cksum)" = "$before" ] || fail "wifi/key not kept"
[ "$(grep -c '^ssh-ed25519' "$C/authorized_keys")" = 1 ] || fail "authorized_keys not kept"
[ "$(cat "$C/lapcam")" = newer ] || fail "program not replaced"

echo "7. bad options change nothing on a card"
snapshot=$(cd "$C" && ls -A | LC_ALL=C sort | xargs -I{} sh -c 'cksum "{}"' | cksum)
tabpass=$(printf 'tab\there1')
for args in "-w ab short" "-w ab 'p\"q12345678'" "-w ab \"\$tabpass\"" "-k 'bad key!'" "-k -u" "-c ../x" "-w '' 12345678"; do
	if eval sh "$M" -u $args "\"$C\"" >/dev/null 2>&1; then fail "accepted: $args"; fi
	[ "$(cd "$C" && ls -A | LC_ALL=C sort | xargs -I{} sh -c 'cksum "{}"' | cksum)" = "$snapshot" ] || fail "card changed by: $args"
done
no_tmp "$C" bad-options

echo "8. a non-empty directory that is not a card is refused"
D=$T/notacard
mkdir -p "$D"
echo precious >"$D/notes.txt"
if sh "$M" -u "$D" >/dev/null 2>&1; then fail "wrote into a random directory"; fi
[ "$(ls "$D")" = "notes.txt" ] || fail "random directory changed"

echo "9. missing dist/ files are reported"
mv "$REPO/dist/beacon" "$REPO/dist/beacon.away"
C=$(card nodist)
if sh "$M" -u "$C" >/dev/null 2>&1; then fail "ran without dist/beacon"; fi
[ -z "$(ls -A "$C")" ] || fail "wrote without dist/beacon"
mv "$REPO/dist/beacon.away" "$REPO/dist/beacon"

echo "10. a wrong image size is refused"
head -c 100 /dev/zero >"$REPO/dist/firmware/thingino-other_cam.bin"
if sh "$M" -u -c other_cam "$C" >/dev/null 2>&1; then fail "accepted a 100-byte image"; fi

echo "11. a failed copy leaves no half file and, with -u -s, keeps the marker"
C=$(card failcopy)
chmod 000 "$REPO/dist/osd-feed"
if sh "$M" -u -s "$C" >/dev/null 2>&1; then chmod 644 "$REPO/dist/osd-feed"; fail "copy of an unreadable program succeeded"; fi
chmod 644 "$REPO/dist/osd-feed"
[ -f "$C/autoupdate-full.done" ] || fail "marker missing after a failed run"
no_tmp "$C" failcopy

echo "12. a failed copy on a fresh -u card leaves the marker (the card must not flash)"
C=$(card failcopy-u)
chmod 000 "$REPO/dist/osd-feed"
if sh "$M" -u "$C" >/dev/null 2>&1; then chmod 644 "$REPO/dist/osd-feed"; fail "copy of an unreadable program succeeded (-u)"; fi
chmod 644 "$REPO/dist/osd-feed"
[ -f "$C/autoupdate-full.done" ] || fail "-u: no marker after a failed run: the card would flash"
no_tmp "$C" failcopy-u

echo "13. a failed copy on a stock card leaves no installer (the card must not install)"
C=$(card failcopy-stock)
chmod 000 "$REPO/dist/osd-feed"
if sh "$M" "$C" >/dev/null 2>&1; then chmod 644 "$REPO/dist/osd-feed"; fail "copy of an unreadable program succeeded (stock)"; fi
chmod 644 "$REPO/dist/osd-feed"
[ ! -f "$C/factory_t31_ZMC6tiIDQN" ] || fail "stock: the installer is on the card after a failed run"
no_tmp "$C" failcopy-stock

echo "all tests passed"
