#!/bin/sh
# install-lapcam.sh - put lapcam (docs/lapcam.md) on the camera, in place of
# Thingino's web UI. Run on the PC, after device/lapcam/build.sh:
#   device/install-lapcam.sh root@<camera-ip> [dist/lapcam]
#
# The binary (about 9 MB) goes to the SD card, which must be mounted at
# /mnt/mmcblk0p1; only the small init script goes to the overlay. Unless the
# card already has a lapcam.args, one is written with "-listen :80": the init
# script then stops Thingino's web server uhttpd (ports 80/443) whenever it
# starts the app, and the app answers on http://<camera-ip>/. uhttpd stays
# enabled at boot, so a camera without the card (or the binary) still has
# Thingino's UI. To run the app on port 8000 next to Thingino's UI instead,
# put "-listen :8000" in lapcam.args first.
# The camera's root password must be a $6$ (SHA-512) hash in /etc/shadow (the
# default; "passwd" on the camera may write another kind): checked first.
# lapcam is (re)started at the end and must answer. Undo (Thingino's UI comes back):
#   ssh root@<camera-ip> '/etc/init.d/S97lapcam stop; rm /etc/init.d/S97lapcam /mnt/mmcblk0p1/lapcam /mnt/mmcblk0p1/lapcam.args'

set -e

CAM=$1
BIN=${2:-$(dirname "$0")/../dist/lapcam}
HERE=$(cd "$(dirname "$0")" && pwd)
SD=/mnt/mmcblk0p1

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/lapcam]" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "lapcam binary not found: $BIN (run device/lapcam/build.sh first)" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

if ! ssh "$CAM" "mountpoint -q $SD"; then
	echo "No SD card mounted at $SD on the camera - nothing was changed" >&2
	exit 1
fi

if ! ssh "$CAM" "grep -q '^root:\\\$6\\\$' /etc/shadow"; then
	echo "The camera's root password is not a SHA-512 (\$6\$) hash: lapcam could not check it, nobody could sign in." >&2
	echo "Set the password again with: ssh $CAM 'mkpasswd -m sha-512' (or passwd, if it writes \$6\$) - nothing was changed" >&2
	exit 1
fi

echo "Installing lapcam"
# the program was called "webui" until 2026-11: retire that install, keep its settings
ssh "$CAM" "set -e
if [ -f /etc/init.d/S97webui ]; then
	echo '  retiring the old webui install'
	touch /run/webui-restarting; sh /etc/init.d/S97webui stop >/dev/null 2>&1 || true
	rm -f /run/webui-restarting /etc/init.d/S97webui /run/webui.pid /run/webui-sessions.json $SD/webui
	[ -f $SD/webui.args ] && [ ! -f $SD/lapcam.args ] && mv $SD/webui.args $SD/lapcam.args
	[ -f $SD/webui.json ] && [ ! -f $SD/lapcam.json ] && mv $SD/webui.json $SD/lapcam.json
	rm -f $SD/webui.args $SD/webui.json
fi"
if ! ssh "$CAM" '[ -x /etc/init.d/S97lapcam ] || exit 0; touch /run/lapcam-restarting; /etc/init.d/S97lapcam stop; rm -f /run/lapcam-restarting'; then
	echo "the running lapcam did not stop - nothing was changed" >&2
	exit 1
fi
push "$BIN" "$SD/lapcam" 755
push "$HERE/S97lapcam" /etc/init.d/S97lapcam 755
# A binary that does not run here must show up now, on the PC
if ! ssh "$CAM" "$SD/lapcam -check >/dev/null"; then
	echo "lapcam does not run on the camera (see above)" >&2
	exit 1
fi
if ! ssh "$CAM" "[ -f $SD/lapcam.args ]"; then
	echo "Taking over port 80 from Thingino's web UI (S97lapcam stops uhttpd while the app runs)"
	ssh "$CAM" "printf -- '-listen :80\n' > $SD/lapcam.args"
fi
port=$(ssh "$CAM" "head -n 1 $SD/lapcam.args | sed -n 's/.*-listen *[^:]*:\([0-9]*\).*/\1/p'")
port=${port:-8000}
# an earlier version of this installer disabled uhttpd for good; it is the
# fallback UI now, so it must be able to start at boot again
ssh "$CAM" '[ -f /etc/init.d/S60uhttpd ] && [ ! -x /etc/init.d/S60uhttpd ] && chmod +x /etc/init.d/S60uhttpd; exit 0'
ssh "$CAM" '/etc/init.d/S97lapcam start'
# "started" from start-stop-daemon only means the process was spawned: wait
# for the app to answer, or fail loudly
if ! ssh "$CAM" "i=0; while [ \$i -lt 10 ]; do wget -q -O /dev/null http://127.0.0.1:$port/api/me 2>/dev/null && exit 0; sleep 1; i=\$((i + 1)); done; exit 1"; then
	echo "lapcam did not answer on port $port within 10 s. Log:" >&2
	ssh "$CAM" 'logread | grep lapcam | tail -n 10' >&2
	exit 1
fi
if [ "$port" = 80 ]; then
	echo "Done. Open http://<camera-ip>/ on the phone. Log: ssh $CAM logread | grep lapcam"
else
	echo "Done. Open http://<camera-ip>:$port/ on the phone. Log: ssh $CAM logread | grep lapcam"
fi
