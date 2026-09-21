#!/bin/sh
# install-prudynt-osd.sh - install the prudynt build with the text-file OSD overlay.
# Run on the PC:
#   device/install-prudynt-osd.sh root@<camera-ip> path/to/prudynt
#
# OPTIONAL, and separate from install.sh because it replaces a Thingino program
# (the streamer) at run time. Thingino's own /usr/bin/prudynt is NOT overwritten:
# the new binary is stored as /usr/bin/prudynt-osd and S30prudynt-osd bind-mounts
# it over /usr/bin/prudynt at boot. Undo:
#   ssh root@<camera-ip> 'rm /etc/init.d/S30prudynt-osd /etc/init.d/S93osd-config /usr/bin/prudynt-osd /usr/bin/prudynt-osd.build /usr/sbin/osd-config; reboot'
#
# prudynt is restarted at the end, which interrupts the stream for a few seconds
# (the youtube-relay supervisor reconnects by itself).

set -e

CAM=$1
BIN=$2
HERE=$(cd "$(dirname "$0")" && pwd)

if [ -z "$CAM" ] || [ -z "$BIN" ]; then
	echo "Usage: $0 root@<camera-ip> path/to/prudynt" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "prudynt binary not found: $BIN" >&2
	exit 1
fi

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

echo "Installing prudynt-osd"
ssh "$CAM" 'df -h /overlay | tail -1'
# Upload next to the final name first: the slow part happens while the stream is
# still up, and the file prudynt is running from (bind-mounted over
# /usr/bin/prudynt) is not touched while it is mounted. Replacing a mounted file
# leaves a stale "(deleted)" mount behind.
push "$BIN" /usr/bin/prudynt-osd.upload 755
want=$(local_md5 "$BIN")
got=$(ssh "$CAM" 'md5sum </usr/bin/prudynt-osd.upload' | cut -d' ' -f1)
if [ -z "$want" ] || [ "$want" != "$got" ]; then
	ssh "$CAM" 'rm -f /usr/bin/prudynt-osd.upload' || true
	echo "md5 mismatch after transfer (overlay full?)" >&2
	exit 1
fi
ssh "$CAM" "echo '$SUPPORTED_BUILD' > /usr/bin/prudynt-osd.build"
push "$HERE/S30prudynt-osd" /etc/init.d/S30prudynt-osd 755
push "$HERE/osd-progress-demo" /usr/sbin/osd-progress-demo 755
# osd-config re-sends the OSD settings from prudynt-osd.json (SD card, then /etc)
# after every prudynt restart. It does nothing until such a file exists.
ssh "$CAM" '[ -x /etc/init.d/S93osd-config ] && /etc/init.d/S93osd-config stop >/dev/null 2>&1; true'
push "$HERE/osd-config" /usr/sbin/osd-config 755
push "$HERE/S93osd-config" /etc/init.d/S93osd-config 755

echo "Switching prudynt (the stream drops for a few seconds)"
# Failures here are not fatal on purpose: the check below decides, and rolls back.
# "S31prudynt stop" returns before prudynt is gone; starting too early makes the new
# one quit with "Another Prudynt instance appears to be running".
STOP_PRUDYNT='/etc/init.d/S31prudynt stop; n=0; while pidof prudynt >/dev/null && [ $n -lt 20 ]; do n=$((n + 1)); sleep 1; done'
# If prudynt cannot be stopped, the old binary stays mounted and running, and the
# check below would happily report success for it: give up instead.
if ! ssh "$CAM" "$STOP_PRUDYNT"'; ! pidof prudynt >/dev/null'; then
	ssh "$CAM" 'rm -f /usr/bin/prudynt-osd.upload; /etc/init.d/S31prudynt start; /etc/init.d/S93osd-config start' >/dev/null 2>&1 || true
	echo "prudynt did not stop (or the camera could not be reached) - nothing was switched" >&2
	exit 1
fi
ssh "$CAM" '
	/etc/init.d/S30prudynt-osd stop
	mv /usr/bin/prudynt-osd.upload /usr/bin/prudynt-osd
	/etc/init.d/S30prudynt-osd start && /etc/init.d/S31prudynt start' || true

# The patched build answers the osd.textfile query with its settings; stock prudynt returns {}.
i=0
ok=""
while [ $i -lt 15 ]; do
	case $(ssh "$CAM" "prudyntctl json '{\"osd\":{\"textfile\":null}}' 2>/dev/null" || true) in
	*'"textfile"'*)
		ok=1
		break
		;;
	esac
	i=$((i + 1))
	sleep 1
done
if [ -z "$ok" ]; then
	echo "prudynt did not come up with osd.textfile support - going back to the stock prudynt" >&2
	# osd-config is pointless (and would keep poking the stock prudynt) without the patch
	ssh "$CAM" 'chmod -x /etc/init.d/S93osd-config; '"$STOP_PRUDYNT"'; /etc/init.d/S30prudynt-osd stop; chmod -x /etc/init.d/S30prudynt-osd; /etc/init.d/S31prudynt start' || true
	exit 1
fi
ssh "$CAM" '/etc/init.d/S93osd-config start' || echo "warning: osd-config did not start (prudynt itself is fine)" >&2
echo "Done. Try it:  ssh $CAM osd-progress-demo 20"
echo "Settings that survive restarts: put prudynt-osd.json on the SD card (see docs/osd.md)"
