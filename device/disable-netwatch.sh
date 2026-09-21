#!/bin/sh
# disable-netwatch.sh - turn off Thingino's network watchdog on a camera.
# Run on the PC:
#   device/disable-netwatch.sh root@<camera-ip>
#
# Thingino (2026-09 and later) reboots the whole OS when the default gateway
# stops answering ping (3 x 30s). For a live stream that is worse than the
# outage itself: the supervisor already waits for the network and resumes.
#
# This changes a Thingino OS setting (/etc/thingino.json), which is why it is
# NOT part of install.sh. Undo with:
#   ssh root@<camera-ip> 'jct /etc/thingino.json set netwatch.enabled true; service restart netwatch'

set -e

CAM=$1
if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip>" >&2
	exit 1
fi

. "$(dirname "$0")/common.sh"
check_camera "$CAM"

echo "Before: $(ssh "$CAM" 'jct /etc/thingino.json get netwatch.enabled 2>/dev/null || echo "(unset = enabled)"')"
ssh "$CAM" 'jct /etc/thingino.json set netwatch.enabled false && /etc/init.d/S52netwatch restart' || true

# "|| echo" keeps set -e from killing the script silently on a failed lookup.
# Off means exactly what S52netwatch itself treats as off: false | 0 | no.
state=$(ssh "$CAM" 'jct /etc/thingino.json get netwatch.enabled 2>/dev/null' || echo "?")
# Look for the watch loop itself, not its pidfile (stop removes that unconditionally)
running=$(ssh "$CAM" 'ps | grep -q "[S]52netwatch" && echo yes || echo no' || echo "?")
echo "After:  enabled=$state, watchdog loop running=$running"
case "$state" in
	false | 0 | no) ;;
	*) state_bad=1 ;;
esac
if [ -n "$state_bad" ] || [ "$running" != "no" ]; then
	echo "netwatch is still active - check the camera manually" >&2
	exit 1
fi
echo "netwatch is off (persists across reboots)."
