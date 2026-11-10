#!/bin/sh
# make-sdcard.sh - put everything lapcam needs on an SD card (docs/sdcard.md).
# Run on the PC, with the card mounted (FAT32):
#   firmware/make-sdcard.sh [options] <card dir>
#     <card dir>      the mounted card (macOS: /Volumes/<name>; Linux: /media/...)
#                     or any directory, to look at the result first
#   -u               for a camera that already runs Thingino (or lapcam): the
#                    image goes on the card as autoupdate-full.bin, which
#                    Thingino's bootloader flashes at the next power-on. Without
#                    -u the card is for a stock Wyze Cam v3: the installer of
#                    thingino-installers (factory_t31_ZMC6tiIDQN, downloaded and
#                    cached in dist/installer/) plus the image under the name it
#                    expects, thingino-<camera>.bin
#     -w SSID PASS   a Wi-Fi network for wpa_supplicant.conf (repeat for more;
#                    the first one is preferred). Without -w an existing file on
#                    the card is kept, else none is written and the camera opens
#                    its own network at boot
#     -k STREAMKEY   write youtube-relay.json with this YouTube stream key
#                    (else lapcam's Stream tab does it later)
#     -c CAMERA      Thingino camera name (default wyze_cam3_t31x_gc2053_atbm6031)
#     -s             with -u: the camera already runs this image (e.g. the card
#                    is being remade); autoupdate-full.done is written too, so
#                    the bootloader does not flash it again
#
# Needs, in dist/: firmware/thingino-<camera>.bin (firmware/build.sh), lapcam,
# osd-feed, beacon (device/*/build.sh) and ffmpeg (firmware/build.sh). Programs
# and the image are always (re)written; settings already on the card
# (wpa_supplicant.conf, youtube-relay.json, prudynt-osd.json, osd-feed.json,
# lapcam.args, logo.png) are kept unless an option says otherwise, so the
# script also updates a card that is in use. Markers that would stop the
# bootloader or the installer from acting again (autoupdate-full.done,
# factory_t31_ZMC6tiIDQN.old) are removed.
#
# The card must be FAT32 (the stock bootloader and the installer read nothing
# else; recordings are short clips, so the 4 GB file limit does not matter).
# macOS: diskutil eraseDisk FAT32 LAPCAM MBRFormat /dev/diskN

set -e

HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/.." && pwd)
CAMERA=wyze_cam3_t31x_gc2053_atbm6031
UPGRADE=
SKIP_FLASH=
WIFI=
KEY=
INSTALLER_URL=https://raw.githubusercontent.com/wltechblog/thingino-installers/main/wyze-cam-3/wyze-cam-3-sd.zip
FACTORY=factory_t31_ZMC6tiIDQN

usage() {
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	-u) UPGRADE=1 ;;
	-s) SKIP_FLASH=1 ;;
	-w)
		[ $# -ge 3 ] || usage
		WIFI="$WIFI
$2	$3"
		shift 2
		;;
	-k)
		[ $# -ge 2 ] || usage
		KEY=$2
		shift
		;;
	-c)
		[ $# -ge 2 ] || usage
		CAMERA=$2
		shift
		;;
	-h | --help) usage ;;
	-*) usage ;;
	*) break ;;
	esac
	shift
done
[ $# -eq 1 ] || usage
CARD=$1
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

# the card's filesystem, when the directory is a mount point
fstype=""
if command -v diskutil >/dev/null 2>&1; then
	fstype=$(diskutil info "$CARD" 2>/dev/null | sed -n 's/.*File System Personality: *//p')
elif command -v findmnt >/dev/null 2>&1; then
	fstype=$(findmnt -n -o FSTYPE --target "$CARD" 2>/dev/null)
	[ "$(findmnt -n -o TARGET --target "$CARD" 2>/dev/null)" = "$(cd "$CARD" && pwd -P)" ] || fstype=""
fi
case "$fstype" in
"") echo "Writing to $CARD (not a mounted card, or its filesystem is unknown here: it must end up on a FAT32 card)" ;;
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

# put <src> <name> - always written
put() {
	cp "$1" "$CARD/$2.tmp"
	mv -f "$CARD/$2.tmp" "$CARD/$2"
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
	rm -f "$CARD/autoupdate-full.done" "$CARD/uenv.done"
	put "$IMAGE" autoupdate-full.bin
	if [ -n "$SKIP_FLASH" ]; then
		printf '1' >"$CARD/autoupdate-full.done"
		echo "  autoupdate-full.done (the image is NOT flashed at boot; remove the file to flash)"
	fi
	# an installer left from a stock install would be harmless (Thingino's
	# bootloader ignores it) but confusing next to autoupdate-full.bin
	rm -f "$CARD/$FACTORY" "$CARD/$FACTORY.old" "$CARD/thingino-$CAMERA.bin"
else
	cache=$REPO/dist/installer
	if [ ! -f "$cache/$FACTORY" ]; then
		echo "Getting the installer ($INSTALLER_URL)"
		mkdir -p "$cache"
		tmp=$(mktemp -d)
		trap 'rm -rf "$tmp"' EXIT
		curl -fsSL -o "$tmp/sd.zip" "$INSTALLER_URL"
		(cd "$tmp" && unzip -q sd.zip)
		img=$(ls "$tmp"/*.img | head -n 1)
		python3 -I "$HERE/fatextract.py" "$img" "$FACTORY" "$cache/$FACTORY"
		# the image names the SD image's build date; keep it for the record
		echo "$INSTALLER_URL $(date -u +%Y-%m-%d)" >"$cache/SOURCE"
	fi
	rm -f "$CARD/$FACTORY.old" "$CARD/autoupdate-full.bin" "$CARD/autoupdate-full.done" "$CARD/autoupdate-full.yay"
	put "$cache/$FACTORY" "$FACTORY"
	put "$IMAGE" "thingino-$CAMERA.bin"
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

if [ -n "$WIFI" ]; then
	# priority counts down from 10 so the first -w is tried first;
	# scan_ssid=1 finds hidden networks too; the header is what S37wifi-from-sd
	# would add anyway
	nl=$(printf '\nx')
	nl=${nl%x}
	{
		printf 'ctrl_interface=/run/wpa_supplicant\nupdate_config=1\nap_scan=1\n'
		prio=10
		printf '%s\n' "$WIFI" | while IFS="$(printf '\t')" read -r ssid pass; do
			[ -n "$ssid" ] || continue
			case "$ssid$pass" in
			*'"'* | *\\* | *"$nl"*)
				echo "quotes, backslashes and line breaks are not supported in -w values" >&2
				exit 1
				;;
			esac
			if [ ${#pass} -lt 8 ] || [ ${#pass} -gt 63 ]; then
				echo "the Wi-Fi password of '$ssid' must be 8 to 63 characters (WPA2)" >&2
				exit 1
			fi
			printf '\nnetwork={\n\tssid="%s"\n\tpsk="%s"\n\tscan_ssid=1\n\tpriority=%d\n}\n' "$ssid" "$pass" "$prio"
			prio=$((prio - 1))
		done
	} >"$CARD/wpa_supplicant.conf.tmp"
	mv -f "$CARD/wpa_supplicant.conf.tmp" "$CARD/wpa_supplicant.conf"
	echo "  wpa_supplicant.conf ($(grep -c '^network=' "$CARD/wpa_supplicant.conf") network(s))"
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
	case "$KEY" in
	*[!A-Za-z0-9_-]*)
		echo "a YouTube stream key has only letters, digits, - and _" >&2
		exit 1
		;;
	esac
	printf '{\n  "enabled": true,\n  "ffmpeg_opts": "",\n  "ffmpeg_out_opts": "",\n  "rtmp_url": "rtmps://a.rtmps.youtube.com:443/live2",\n  "rtsp_url": "rtsp://thingino:thingino@127.0.0.1:554/ch0",\n  "stream_key": "%s"\n}\n' "$KEY" >"$CARD/youtube-relay.json.tmp"
	mv -f "$CARD/youtube-relay.json.tmp" "$CARD/youtube-relay.json"
	echo "  youtube-relay.json (stream key set)"
elif [ -f "$CARD/youtube-relay.json" ]; then
	echo "  youtube-relay.json (kept)"
else
	echo "  (no youtube-relay.json: set the stream key in lapcam's Stream tab, or -k STREAMKEY)"
fi
[ -f "$CARD/logo.png" ] && echo "  logo.png (kept)" || echo "  (no logo.png: upload one from lapcam's OSD tab)"

sync
echo
if [ -n "$SKIP_FLASH" ]; then
	echo "Done. Camera off, card in, camera on: nothing is flashed (autoupdate-full.done is on the card)."
elif [ -n "$UPGRADE" ]; then
	echo "Done. Camera off, card in, camera on: the bootloader flashes autoupdate-full.bin and reboots (about 2 minutes)."
else
	echo "Done. Camera off, card in, camera on: the installer backs the stock firmware up to the card and flashes lapcam (a few minutes, LED red/blue/purple), then the camera reboots."
fi
echo "Then http://lapcam.local/ (user root, Thingino's default password root)."
command -v diskutil >/dev/null 2>&1 && echo "Eject first: diskutil eject \"$CARD\""
exit 0
