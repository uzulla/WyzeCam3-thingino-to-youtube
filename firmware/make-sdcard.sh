#!/bin/sh
# make-sdcard.sh - put everything lapcam needs on an SD card (docs/sdcard.md).
# Run on the PC, with the card mounted (FAT32):
#   firmware/make-sdcard.sh [options] <card dir>
#     <card dir>      the mounted card (macOS: /Volumes/<name>; Linux: /media/...).
#                     A plain directory is accepted when it is empty or holds a
#                     card made earlier by this script (to look at the result)
#   -u               for a camera that already runs Thingino (or lapcam): the
#                    image goes on the card as autoupdate-full.bin, which
#                    Thingino's bootloader flashes at the next power-on. Without
#                    -u the card is for a stock Wyze Cam v3: the installer of
#                    thingino-installers (factory_t31_ZMC6tiIDQN, downloaded,
#                    checked and cached in dist/installer/) plus the image under
#                    the name it expects, thingino-<camera>.bin
#     -s             with -u: the camera already runs this image (e.g. the card
#                    is being remade); autoupdate-full.done is written too, so
#                    the bootloader does not flash it again. The card may then
#                    be exFAT as well (the bootloader never reads it)
#     -w SSID PASS   a Wi-Fi network for wpa_supplicant.conf (repeat for more;
#                    the first one is preferred). Without -w an existing file on
#                    the card is kept, else none is written and the camera opens
#                    its own network at boot
#     -k STREAMKEY   write youtube-relay.json with this YouTube stream key
#                    (else lapcam's Stream tab does it later)
#     -c CAMERA      Thingino camera name (default wyze_cam3_t31x_gc2053_atbm6031)
#
# Needs, in dist/: firmware/thingino-<camera>.bin (firmware/build.sh), lapcam,
# osd-feed, beacon (device/*/build.sh) and ffmpeg (firmware/build.sh). Programs
# and the image are always (re)written; settings already on the card
# (wpa_supplicant.conf, youtube-relay.json, prudynt-osd.json, osd-feed.json,
# lapcam.args, logo.png) are kept unless an option says otherwise, so the
# script also updates a card that is in use. All options are checked before
# the card is touched, and the markers that make the bootloader or the
# installer skip the card (autoupdate-full.done, factory_t31_ZMC6tiIDQN.old)
# are handled last, so a card left by a failed run never flashes by mistake.
#
# The card must be FAT32 (the stock bootloader and the installer read nothing
# else; recordings are short clips, so the 4 GB file limit does not matter).
# macOS: diskutil eraseDisk FAT32 LAPCAM MBRFormat /dev/diskN

set -e
umask 022

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/.." && pwd)
CAMERA=wyze_cam3_t31x_gc2053_atbm6031
UPGRADE=
SKIP_FLASH=
WIFI=""
NWIFI=0
KEY=
# thingino-installers, pinned to the commit of the SD image read for
# docs/sdcard.md (2026-08-14); the file taken out of it is checked by hash.
# To follow a newer installer: change both, after reading its init script.
INSTALLER_URL=https://raw.githubusercontent.com/wltechblog/thingino-installers/4d8530f6abc24ca8d1d12a0323cc36f02fee2e1c/wyze-cam-3/wyze-cam-3-sd.zip
FACTORY=factory_t31_ZMC6tiIDQN
FACTORY_SHA256=6562813c17ac44dabf1edd250fd7fd3b702e98eafeb486405225ed59f87dec6e

usage() {
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit "${1:-1}"
}

nl=$(printf '\nx')
nl=${nl%x}
tab=$(printf '\t')

# check_wifi <ssid> <pass>: what the generated wpa_supplicant.conf can carry
check_wifi() {
	case "$1$2" in
	*'"'* | *\\* | *"$nl"* | *"$tab"*)
		echo "quotes, backslashes, tabs and line breaks are not supported in -w values" >&2
		exit 1
		;;
	esac
	[ -n "$1" ] || { echo "-w: the SSID is empty" >&2; exit 1; }
	if [ "$(printf '%s' "$1" | LC_ALL=C wc -c)" -gt 32 ]; then
		echo "-w: an SSID has 32 bytes at most: '$1'" >&2
		exit 1
	fi
	n=$(printf '%s' "$2" | LC_ALL=C wc -c)
	if [ "$n" -lt 8 ] || [ "$n" -gt 63 ]; then
		echo "-w: the Wi-Fi password of '$1' must be 8 to 63 characters (WPA2)" >&2
		exit 1
	fi
}

while [ $# -gt 0 ]; do
	case "$1" in
	-u) UPGRADE=1 ;;
	-s) SKIP_FLASH=1 ;;
	-w)
		[ $# -ge 3 ] || usage
		check_wifi "$2" "$3"
		WIFI="$WIFI$2$tab$3$nl"
		NWIFI=$((NWIFI + 1))
		shift 2
		;;
	-k)
		[ $# -ge 2 ] || usage
		KEY=$2
		case "$KEY" in
		"" | *[!A-Za-z0-9_-]* | -*)
			echo "a YouTube stream key has only letters, digits, - and _: '$KEY'" >&2
			exit 1
			;;
		esac
		shift
		;;
	-c)
		[ $# -ge 2 ] || usage
		CAMERA=$2
		shift
		;;
	-h | --help) usage 0 ;;
	-*) usage ;;
	*) break ;;
	esac
	shift
done
[ $# -eq 1 ] || usage
CARD=$1
case "$CAMERA" in
"" | *[!A-Za-z0-9_-]*) echo "not a camera name: '$CAMERA'" >&2; exit 1 ;;
esac
if [ -n "$SKIP_FLASH" ] && [ -z "$UPGRADE" ]; then
	echo "-s goes with -u" >&2
	exit 1
fi
if [ ! -d "$CARD" ]; then
	echo "not a directory: $CARD" >&2
	exit 1
fi

# what has to be built first
IMAGE=$REPO/dist/firmware/thingino-$CAMERA.bin
missing=0
for f in "$IMAGE" "$REPO/dist/lapcam" "$REPO/dist/osd-feed" "$REPO/dist/beacon" "$REPO/dist/ffmpeg"; do
	if [ ! -f "$f" ]; then
		echo "missing: $f" >&2
		missing=1
	fi
done
if [ $missing = 1 ]; then
	echo "build first: firmware/build.sh (image, ffmpeg), device/lapcam/build.sh, device/osd-feed/build.sh, device/beacon/build.sh" >&2
	exit 1
fi
if [ "$(wc -c <"$IMAGE" | tr -d ' ')" != 16777216 ]; then
	echo "not a 16 MB flash image: $IMAGE" >&2
	exit 1
fi
if [ -z "$UPGRADE" ]; then
	for t in curl unzip python3; do
		if ! command -v $t >/dev/null 2>&1; then
			echo "$t is needed to take the installer out of thingino-installers' image" >&2
			exit 1
		fi
	done
fi

# the card's filesystem, when the directory is a mount point
fstype=""
if command -v diskutil >/dev/null 2>&1; then
	fstype=$(diskutil info "$CARD" 2>/dev/null | sed -n 's/.*File System Personality: *//p') || true
elif command -v findmnt >/dev/null 2>&1; then
	fstype=$(findmnt -n -o FSTYPE --target "$CARD" 2>/dev/null) || true
	[ "$(findmnt -n -o TARGET --target "$CARD" 2>/dev/null)" = "$(cd "$CARD" && pwd -P)" ] || fstype=""
fi
case "$fstype" in
"")
	# a plain directory: only an empty one, or a card this script made
	# (anything else could be some directory named by mistake)
	if [ -n "$(ls -A "$CARD" | grep -v '^\.' | head -n 1)" ] && ! grep -q '^lapcam SD card' "$CARD/README.txt" 2>/dev/null; then
		echo "$CARD is not a mounted card and not empty: refusing to write into it" >&2
		exit 1
	fi
	echo "Writing to $CARD (a directory, not a mounted card: copy the result to a FAT32 card)"
	;;
*FAT32* | vfat | msdos) echo "Writing to $CARD ($fstype)" ;;
*)
	if [ -n "$SKIP_FLASH" ]; then
		# nothing is flashed from this card, so only the camera's kernel has
		# to read it (it mounts exFAT too); a later update needs a FAT32 card
		echo "Writing to $CARD ($fstype: the bootloader cannot read this, fine with -s; use a FAT32 card to flash)"
	else
		echo "$CARD is $fstype, not FAT32: neither the stock bootloader nor Thingino's can read it." >&2
		echo "Format the card as FAT32 first (macOS: diskutil eraseDisk FAT32 LAPCAM MBRFormat /dev/diskN)" >&2
		exit 1
	fi
	;;
esac

# half-written files never stay behind (the card may be in use)
tmpdir=""
cleanup() {
	rm -f "$CARD"/*.lapcam-tmp
	if [ -n "$tmpdir" ]; then
		rm -rf "$tmpdir"
	fi
	# (a failing last command here would become the script's exit status)
	return 0
}
trap cleanup EXIT

# put <src> <name> - always written
put() {
	cp "$1" "$CARD/$2.lapcam-tmp"
	mv -f "$CARD/$2.lapcam-tmp" "$CARD/$2"
	echo "  $2"
}
# keep_or_put <src> <name> - a file already on the card wins
keep_or_put() {
	if [ -f "$CARD/$2" ]; then
		echo "  $2 (kept)"
	else
		put "$1" "$2"
	fi
}

# --- firmware ---------------------------------------------------------------

if [ -n "$UPGRADE" ]; then
	if [ -n "$SKIP_FLASH" ]; then
		# the marker comes first: should anything below fail, the card still
		# says "already applied" and the bootloader leaves the flash alone
		printf '1' >"$CARD/autoupdate-full.done"
		echo "  autoupdate-full.done (the image is NOT flashed at boot; remove the file to flash)"
	fi
	put "$IMAGE" autoupdate-full.bin
	# leftovers of a stock install on this card: harmless to Thingino's
	# bootloader (it ignores them) but confusing next to autoupdate-full.bin
	rm -f "$CARD/$FACTORY" "$CARD/$FACTORY.old" "$CARD/thingino-$CAMERA.bin" "$CARD/autoupdate-full.yay"
else
	cache=$REPO/dist/installer
	if [ ! -f "$cache/$FACTORY" ] || [ ! -f "$cache/SOURCE" ]; then
		echo "Getting the installer ($INSTALLER_URL)"
		mkdir -p "$cache"
		tmpdir=$(mktemp -d)
		curl -fsSL -o "$tmpdir/sd.zip" "$INSTALLER_URL"
		(cd "$tmpdir" && unzip -q sd.zip)
		img=$(find "$tmpdir" -name '*.img' | head -n 1)
		if [ -z "$img" ]; then
			echo "no .img in the installer's zip: $INSTALLER_URL" >&2
			exit 1
		fi
		python3 -I "$HERE/fatextract.py" "$img" "$FACTORY" "$tmpdir/$FACTORY"
		if command -v shasum >/dev/null 2>&1; then
			sum=$(shasum -a 256 "$tmpdir/$FACTORY" | cut -d' ' -f1)
		else
			sum=$(sha256sum "$tmpdir/$FACTORY" | cut -d' ' -f1)
		fi
		if [ "$sum" != "$FACTORY_SHA256" ]; then
			echo "the installer taken out of the image is not the one this script was checked with:" >&2
			echo "  sha256 $sum, expected $FACTORY_SHA256 (see INSTALLER_URL in this script)" >&2
			exit 1
		fi
		rm -f "$cache/SOURCE"
		mv "$tmpdir/$FACTORY" "$cache/$FACTORY"
		printf '%s\nsha256 %s\nfetched %s\n' "$INSTALLER_URL" "$sum" "$(date -u +%Y-%m-%dT%H:%MZ)" >"$cache/SOURCE"
	fi
	# the installer renames itself to .old when it has run, and the image to
	# .yay before flashing it: both would stop or confuse a new install
	rm -f "$CARD/$FACTORY.old" "$CARD/autoupdate-full.bin" "$CARD/autoupdate-full.done" "$CARD/autoupdate-full.yay" "$CARD/uenv.done"
	put "$IMAGE" "thingino-$CAMERA.bin"
	put "$cache/$FACTORY" "$FACTORY"
fi

# --- programs ---------------------------------------------------------------

echo "Programs"
for p in lapcam osd-feed beacon ffmpeg; do
	put "$REPO/dist/$p" "$p"
done

# --- settings ---------------------------------------------------------------

echo "Settings"
if [ ! -f "$CARD/lapcam.args" ]; then
	echo "-listen :80" >"$CARD/lapcam.args"
	echo "  lapcam.args"
else
	echo "  lapcam.args (kept)"
fi
keep_or_put "$HERE/sdcard/prudynt-osd.json" prudynt-osd.json
keep_or_put "$HERE/sdcard/osd-feed.json" osd-feed.json
put "$HERE/sdcard/README.txt" README.txt

if [ "$NWIFI" -gt 0 ]; then
	# priority counts down from 10 so the first -w is tried first;
	# scan_ssid=1 finds hidden networks too; the header is what S37wifi-from-sd
	# would add anyway. The values were checked when the options were read.
	{
		printf 'ctrl_interface=/run/wpa_supplicant\nupdate_config=1\nap_scan=1\n'
		prio=10
		printf '%s' "$WIFI" | while IFS="$tab" read -r ssid pass; do
			printf '\nnetwork={\n\tssid="%s"\n\tpsk="%s"\n\tscan_ssid=1\n\tpriority=%d\n}\n' "$ssid" "$pass" "$prio"
			prio=$((prio - 1))
		done
	} >"$CARD/wpa_supplicant.conf.lapcam-tmp"
	mv -f "$CARD/wpa_supplicant.conf.lapcam-tmp" "$CARD/wpa_supplicant.conf"
	echo "  wpa_supplicant.conf ($NWIFI network(s))"
elif [ -f "$CARD/wpa_supplicant.conf" ]; then
	echo "  wpa_supplicant.conf (kept)"
else
	echo "  (no wpa_supplicant.conf: the camera will open its own Wi-Fi network at boot; -w SSID PASS writes one)"
fi
# Thingino's own one-network uenv.txt would fight with wpa_supplicant.conf
if [ -f "$CARD/uenv.txt" ] && grep -q '^wlanssid=' "$CARD/uenv.txt"; then
	echo "  WARNING: uenv.txt on the card sets wlanssid; Thingino applies it after wpa_supplicant.conf. Remove one of the two." >&2
fi

if [ -n "$KEY" ]; then
	printf '{\n  "enabled": true,\n  "ffmpeg_opts": "",\n  "ffmpeg_out_opts": "",\n  "rtmp_url": "rtmps://a.rtmps.youtube.com:443/live2",\n  "rtsp_url": "rtsp://thingino:thingino@127.0.0.1:554/ch0",\n  "stream_key": "%s"\n}\n' "$KEY" >"$CARD/youtube-relay.json.lapcam-tmp"
	mv -f "$CARD/youtube-relay.json.lapcam-tmp" "$CARD/youtube-relay.json"
	echo "  youtube-relay.json (stream key set)"
elif [ -f "$CARD/youtube-relay.json" ]; then
	echo "  youtube-relay.json (kept)"
else
	echo "  (no youtube-relay.json: set the stream key in lapcam's Stream tab, or -k STREAMKEY)"
fi
[ -f "$CARD/logo.png" ] && echo "  logo.png (kept)" || echo "  (no logo.png: upload one from lapcam's OSD tab)"

# what is on the card, by version (each build script leaves one next to its
# output); a release has the same number everywhere (docs/release.md)
{
	echo "lapcam SD card, made $(date -u +%Y-%m-%dT%H:%MZ) by firmware/make-sdcard.sh"
	echo
	first=""
	mixed=""
	for p in firmware/VERSION lapcam.version osd-feed.version beacon.version; do
		v=$(cat "$REPO/dist/$p" 2>/dev/null || echo unknown)
		case "$p" in
		firmware/VERSION) n="firmware (thingino-$CAMERA.bin, Thingino $(cat "$REPO/dist/firmware/BUILD" 2>/dev/null || echo "build unknown"))" ;;
		*) n=${p%.version} ;;
		esac
		echo "$n: $v"
		[ -n "$first" ] || first=$v
		[ "$v" = "$first" ] || mixed=1
	done
	echo "ffmpeg: built with the image"
} >"$CARD/VERSION.txt.lapcam-tmp"
mv -f "$CARD/VERSION.txt.lapcam-tmp" "$CARD/VERSION.txt"
if [ -n "$mixed" ]; then
	echo "  VERSION.txt - WARNING: the image and the programs are from different builds:" >&2
	sed 's/^/    /' "$CARD/VERSION.txt" >&2
else
	echo "  VERSION.txt ($first)"
fi

# only now, with everything in place, may the bootloader act on the card
if [ -n "$UPGRADE" ] && [ -z "$SKIP_FLASH" ]; then
	rm -f "$CARD/autoupdate-full.done" "$CARD/uenv.done"
fi
sync
echo
if [ -n "$SKIP_FLASH" ]; then
	echo "Done. Camera off, card in, camera on: nothing is flashed (autoupdate-full.done is on the card)."
elif [ -n "$UPGRADE" ]; then
	echo "Done. Camera off, card in, camera on: the bootloader flashes autoupdate-full.bin and reboots (about 2 minutes, red LED on)."
	echo "A camera on an old Thingino build flashes it silently and cannot leave the .done marker; its new bootloader flashes once more at the next boot (docs/sdcard.md)."
else
	echo "Done. Camera off, card in, camera on: the installer backs the stock firmware up to the card and flashes lapcam (a few minutes, LED red/blue/purple), then the camera reboots."
fi
echo "Then http://lapcam.local/ (user root, Thingino's default password root)."
if [ -n "$fstype" ] && command -v diskutil >/dev/null 2>&1; then
	echo "Eject first: diskutil eject \"$CARD\""
fi
exit 0
