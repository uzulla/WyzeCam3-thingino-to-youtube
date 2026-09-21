#!/bin/sh
# install.sh - push the YouTube Live relay to a Thingino camera over SSH.
# Run on the PC, from anywhere:
#   device/install.sh root@<camera-ip> [path/to/ffmpeg]
#
# Safe to re-run. Meant for the first install AND for after a firmware upgrade:
# rootfs/full upgrades wipe the overlay, which takes /usr/bin/ffmpeg and
# (unless restored from the config backup) the scripts with it.
#
# The ffmpeg argument is optional; without it only scripts are (re)installed.
# An existing /etc/youtube-relay.json on the camera is never overwritten.

set -e

CAM=$1
FFMPEG=$2
HERE=$(cd "$(dirname "$0")" && pwd)

# Paths kept across upgrades via Thingino's cfg-backup (64KB raw partition).
# ffmpeg itself (2.5MB) cannot fit there - it must be re-pushed or live on SD.
BACKUP_PATHS="/etc/youtube-relay.json /usr/sbin/youtube-relay /etc/init.d/S93youtube-relay"

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/ffmpeg]" >&2
	exit 1
fi
if [ -n "$FFMPEG" ] && [ ! -f "$FFMPEG" ]; then
	echo "ffmpeg binary not found: $FFMPEG" >&2
	exit 1
fi

# Thingino has no sftp-server, so stream files through ssh instead of scp
push() {
	echo "  $1 -> $2"
	ssh "$CAM" "cat > '$2.new' && chmod $3 '$2.new' && mv '$2.new' '$2'" <"$1"
}

echo "Camera: $(ssh "$CAM" '. /etc/os-release; echo "$IMAGE_ID / $BUILD_ID / gcc$TOOLCHAIN_GCC"')"

if [ -n "$FFMPEG" ]; then
	echo "Installing ffmpeg"
	ssh "$CAM" 'df -h /overlay | tail -1'
	push "$FFMPEG" /usr/bin/ffmpeg 755
	want=$(md5sum <"$FFMPEG" | cut -d' ' -f1)
	got=$(ssh "$CAM" 'md5sum </usr/bin/ffmpeg' | cut -d' ' -f1)
	if [ "$want" != "$got" ]; then
		echo "md5 mismatch after transfer (overlay full?)" >&2
		exit 1
	fi
	if ! ssh "$CAM" '/usr/bin/ffmpeg -hide_banner -protocols 2>/dev/null' | grep -q '^ *rtmps$'; then
		echo "WARNING: installed ffmpeg does not run or lacks rtmps" >&2
		echo "         (toolchain/mbedTLS mismatch with this firmware? see README)" >&2
	fi
fi

echo "Installing scripts"
ssh "$CAM" '/etc/init.d/S93youtube-relay stop >/dev/null 2>&1 || true'
push "$HERE/youtube-relay" /usr/sbin/youtube-relay 755
push "$HERE/S93youtube-relay" /etc/init.d/S93youtube-relay 755

if ssh "$CAM" '[ -f /etc/youtube-relay.json ]'; then
	echo "Keeping existing /etc/youtube-relay.json"
else
	push "$HERE/youtube-relay.json.example" /etc/youtube-relay.json 600
	echo "  -> edit stream_key in /etc/youtube-relay.json (or use the SD card instead)"
fi

echo "Registering files in /etc/cfg-backup.list"
for p in $BACKUP_PATHS; do
	ssh "$CAM" "grep -qxF '$p' /etc/cfg-backup.list || echo '$p' >> /etc/cfg-backup.list"
done

# cfg-backup stores an uncompressed tar in one 64KB block behind a 64B header
# (missing paths are skipped, same as cfg-backup does)
size=$(ssh "$CAM" 'tar cf - /etc/cfg-backup.list $(for p in $(grep -v "^#" /etc/cfg-backup.list); do [ -e "$p" ] && echo "$p"; done) 2>/dev/null | wc -c')
echo "Config backup payload: $size / 65472 bytes"
if [ "$size" -gt 65472 ]; then
	echo "WARNING: too large - 'sysupgrade -B' will refuse to back up." >&2
	echo "         Remove the two script paths from /etc/cfg-backup.list and" >&2
	echo "         re-run this installer after each upgrade instead." >&2
fi

ssh "$CAM" '/etc/init.d/S93youtube-relay start'
echo "Done. Logs: ssh $CAM 'logread | grep youtube-relay'"
