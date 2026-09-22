#!/bin/sh
# install-osd-feed.sh - put osd-feed (docs/osd-feed.md) on the camera.
# Run on the PC, after device/osd-feed/build.sh:
#   device/install-osd-feed.sh root@<camera-ip> [dist/osd-feed]
#
# The binary (about 8 MB) and its configuration go to the SD card, which must be
# mounted at /mnt/mmcblk0p1; only the small init script goes to the overlay.
# An existing osd-feed.json on the card is kept; without one the example is
# copied there (it shows memory, a looping bar and the clock).
# osd-feed is (re)started at the end. Undo:
#   ssh root@<camera-ip> '/etc/init.d/S94osd-feed stop; rm /etc/init.d/S94osd-feed /mnt/mmcblk0p1/osd-feed /mnt/mmcblk0p1/osd-feed.json'

set -e

CAM=$1
BIN=${2:-$(dirname "$0")/../dist/osd-feed}
HERE=$(cd "$(dirname "$0")" && pwd)
SD=/mnt/mmcblk0p1

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/osd-feed]" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "osd-feed binary not found: $BIN (run device/osd-feed/build.sh first)" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

if ! ssh "$CAM" "mountpoint -q $SD"; then
	echo "No SD card mounted at $SD on the camera - nothing was changed" >&2
	exit 1
fi

echo "Installing osd-feed"
# Stop first, then replace the binary: simpler than reasoning about renaming
# over a running executable on exFAT
ssh "$CAM" '[ -x /etc/init.d/S94osd-feed ] && /etc/init.d/S94osd-feed stop >/dev/null 2>&1; true'
push "$BIN" "$SD/osd-feed" 755
push "$HERE/S94osd-feed" /etc/init.d/S94osd-feed 755
if ssh "$CAM" "[ -f $SD/osd-feed.json ]"; then
	echo "  keeping the existing $SD/osd-feed.json"
else
	push "$HERE/osd-feed/osd-feed.json.example" "$SD/osd-feed.json" 644
fi
# A bad config must show up now, on the PC, not as a silent non-start at boot
if ! ssh "$CAM" "$SD/osd-feed -once" >/dev/null; then
	echo "osd-feed refuses $SD/osd-feed.json (see above) - fix it, then: ssh $CAM /etc/init.d/S94osd-feed start" >&2
	exit 1
fi
ssh "$CAM" '/etc/init.d/S94osd-feed start'
echo "Done. Log: ssh $CAM logread | grep osd-feed"
echo "The overlays only show if their slots are enabled (docs/osd.md: prudynt-osd.json or the web UI)."
