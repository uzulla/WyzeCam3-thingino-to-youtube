#!/bin/sh
# install-beacon.sh - put beacon (docs/beacon.md) on the camera.
# Run on the PC, after device/beacon/build.sh:
#   device/install-beacon.sh root@<camera-ip> [dist/beacon]
#
# The binary (about 4 MB) goes to the SD card, which must be mounted at
# /mnt/mmcblk0p1; only the small init script goes to the overlay. An existing
# beacon.args on the card is kept. beacon is (re)started at the end. Undo:
#   ssh root@<camera-ip> '/etc/init.d/S95beacon stop; rm /etc/init.d/S95beacon /mnt/mmcblk0p1/beacon'

set -e

CAM=$1
BIN=${2:-$(dirname "$0")/../dist/beacon}
HERE=$(cd "$(dirname "$0")" && pwd)
SD=/mnt/mmcblk0p1

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/beacon]" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "beacon binary not found: $BIN (run device/beacon/build.sh first)" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

if ! ssh "$CAM" "mountpoint -q $SD"; then
	echo "No SD card mounted at $SD on the camera - nothing was changed" >&2
	exit 1
fi

echo "Installing beacon"
# Stop first, then replace the binary (as install-osd-feed.sh does)
if ! ssh "$CAM" '[ -x /etc/init.d/S95beacon ] || exit 0; /etc/init.d/S95beacon stop'; then
	echo "the running beacon did not stop - nothing was changed" >&2
	exit 1
fi
push "$BIN" "$SD/beacon" 755
push "$HERE/S95beacon" /etc/init.d/S95beacon 755
# A binary that does not run here must show up now, on the PC
if ! ssh "$CAM" "$SD/beacon -once"; then
	echo "beacon does not run on the camera (see above)" >&2
	exit 1
fi
ssh "$CAM" '/etc/init.d/S95beacon start'
echo "Done. Log: ssh $CAM logread | grep beacon"
echo "On a PC on the same network: go -C tools/beacon-listen run .  (commands: go -C tools/beacon-send run . ping)"
