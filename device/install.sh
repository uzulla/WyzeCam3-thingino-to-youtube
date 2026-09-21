#!/bin/sh
# install.sh - push the YouTube Live relay to a Thingino camera over SSH.
# Run on the PC, from anywhere:
#   device/install.sh root@<camera-ip> [path/to/ffmpeg]
#
# Safe to re-run. Meant to be run on a freshly installed (or freshly updated)
# Thingino: reinstalling/updating the firmware removes everything placed here.
#
# The ffmpeg argument is optional; without it only scripts are (re)installed.
# An existing /etc/youtube-relay.json on the camera is never overwritten.
#
# Scope: this only places OUR files (ffmpeg, supervisor, init script, config).
# It never touches Thingino's own files or settings; updating or backing up
# the OS is out of scope.

set -e

CAM=$1
FFMPEG=$2
HERE=$(cd "$(dirname "$0")" && pwd)

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
	if ! ssh "$CAM" "cat > '$2.new' && chmod $3 '$2.new' && mv '$2.new' '$2' || { rm -f '$2.new'; exit 1; }" <"$1"; then
		echo "Failed to write $2 (overlay full? check: ssh $CAM df -h /overlay)" >&2
		exit 1
	fi
}

# md5sum on Linux, md5 on macOS
local_md5() {
	if command -v md5sum >/dev/null 2>&1; then
		md5sum <"$1" | cut -d' ' -f1
	else
		md5 -q "$1"
	fi
}

. "$HERE/common.sh"
check_camera "$CAM"

if [ -n "$FFMPEG" ]; then
	echo "Installing ffmpeg"
	ssh "$CAM" 'df -h /overlay | tail -1'
	push "$FFMPEG" /usr/bin/ffmpeg 755
	want=$(local_md5 "$FFMPEG")
	got=$(ssh "$CAM" 'md5sum </usr/bin/ffmpeg' | cut -d' ' -f1)
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		echo "md5 mismatch after transfer (overlay full?)" >&2
		exit 1
	fi
	if ! ssh "$CAM" '/usr/bin/ffmpeg -hide_banner -protocols 2>/dev/null' | grep -q '^ *rtmps$'; then
		echo "WARNING: installed ffmpeg does not run or lacks rtmps" >&2
		echo "         (toolchain/mbedTLS mismatch with this firmware? see README)" >&2
	fi
fi

echo "Installing scripts"
# stop only signals the supervisor; it exits after its current sleep and after
# reaping ffmpeg. Wait for that, or the new instance would briefly publish to
# the same stream key alongside the old one.
ssh "$CAM" '
	pid=$(cat /run/youtube-relay.pid 2>/dev/null)
	/etc/init.d/S93youtube-relay stop >/dev/null 2>&1
	i=0
	while [ -n "$pid" ] && [ -d "/proc/$pid" ] && [ $i -lt 45 ]; do
		i=$((i + 1))
		sleep 1
	done
	if [ -n "$pid" ] && [ -d "/proc/$pid" ]; then
		echo "old supervisor (pid $pid) did not exit, killing it" >&2
		pkill -P "$pid" 2>/dev/null # its ffmpeg child first, or it would be orphaned
		kill -9 "$pid" 2>/dev/null
	fi
	true
'
push "$HERE/youtube-relay" /usr/sbin/youtube-relay 755
push "$HERE/S93youtube-relay" /etc/init.d/S93youtube-relay 755

if ssh "$CAM" '[ -f /etc/youtube-relay.json ]'; then
	echo "Keeping existing /etc/youtube-relay.json"
else
	push "$HERE/youtube-relay.json.example" /etc/youtube-relay.json 600
	echo "  -> edit stream_key in /etc/youtube-relay.json (or use the SD card instead)"
fi

ssh "$CAM" '/etc/init.d/S93youtube-relay start'
echo "Done. Logs: ssh $CAM 'logread | grep youtube-relay'"
