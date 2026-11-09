#!/bin/sh
# install-webui.sh - put webui (docs/webui.md) on the camera, in place of
# Thingino's web UI. Run on the PC, after device/webui/build.sh:
#   device/install-webui.sh root@<camera-ip> [dist/webui]
#
# The binary (about 9 MB) goes to the SD card, which must be mounted at
# /mnt/mmcblk0p1; only the small init script goes to the overlay. Unless the
# card already has a webui.args, one is written with "-listen :80": the init
# script then stops Thingino's web server uhttpd (ports 80/443) whenever it
# starts the app, and the app answers on http://<camera-ip>/. uhttpd stays
# enabled at boot, so a camera without the card (or the binary) still has
# Thingino's UI. To run the app on port 8000 next to Thingino's UI instead,
# put "-listen :8000" in webui.args first.
# The camera's root password must be a $6$ (SHA-512) hash in /etc/shadow (the
# default; "passwd" on the camera may write another kind): checked first.
# webui is (re)started at the end and must answer. Undo (Thingino's UI comes back):
#   ssh root@<camera-ip> '/etc/init.d/S97webui stop; rm /etc/init.d/S97webui /mnt/mmcblk0p1/webui /mnt/mmcblk0p1/webui.args'

set -e

CAM=$1
BIN=${2:-$(dirname "$0")/../dist/webui}
HERE=$(cd "$(dirname "$0")" && pwd)
SD=/mnt/mmcblk0p1

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/webui]" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "webui binary not found: $BIN (run device/webui/build.sh first)" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

if ! ssh "$CAM" "mountpoint -q $SD"; then
	echo "No SD card mounted at $SD on the camera - nothing was changed" >&2
	exit 1
fi

if ! ssh "$CAM" "grep -q '^root:\\\$6\\\$' /etc/shadow"; then
	echo "The camera's root password is not a SHA-512 (\$6\$) hash: webui could not check it, nobody could sign in." >&2
	echo "Set the password again with: ssh $CAM 'mkpasswd -m sha-512' (or passwd, if it writes \$6\$) - nothing was changed" >&2
	exit 1
fi

echo "Installing webui"
if ! ssh "$CAM" '[ -x /etc/init.d/S97webui ] || exit 0; touch /run/webui-restarting; /etc/init.d/S97webui stop; rm -f /run/webui-restarting'; then
	echo "the running webui did not stop - nothing was changed" >&2
	exit 1
fi
push "$BIN" "$SD/webui" 755
push "$HERE/S97webui" /etc/init.d/S97webui 755
# A binary that does not run here must show up now, on the PC
if ! ssh "$CAM" "$SD/webui -check >/dev/null"; then
	echo "webui does not run on the camera (see above)" >&2
	exit 1
fi
if ! ssh "$CAM" "[ -f $SD/webui.args ]"; then
	echo "Taking over port 80 from Thingino's web UI (S97webui stops uhttpd while the app runs)"
	ssh "$CAM" "printf -- '-listen :80\n' > $SD/webui.args"
fi
port=$(ssh "$CAM" "head -n 1 $SD/webui.args | sed -n 's/.*-listen *[^:]*:\([0-9]*\).*/\1/p'")
port=${port:-8000}
# an earlier version of this installer disabled uhttpd for good; it is the
# fallback UI now, so it must be able to start at boot again
ssh "$CAM" '[ -f /etc/init.d/S60uhttpd ] && [ ! -x /etc/init.d/S60uhttpd ] && chmod +x /etc/init.d/S60uhttpd; exit 0'
ssh "$CAM" '/etc/init.d/S97webui start'
# "started" from start-stop-daemon only means the process was spawned: wait
# for the app to answer, or fail loudly
if ! ssh "$CAM" "i=0; while [ \$i -lt 10 ]; do wget -q -O /dev/null http://127.0.0.1:$port/api/me 2>/dev/null && exit 0; sleep 1; i=\$((i + 1)); done; exit 1"; then
	echo "webui did not answer on port $port within 10 s. Log:" >&2
	ssh "$CAM" 'logread | grep webui | tail -n 10' >&2
	exit 1
fi
if [ "$port" = 80 ]; then
	echo "Done. Open http://<camera-ip>/ on the phone. Log: ssh $CAM logread | grep webui"
else
	echo "Done. Open http://<camera-ip>:$port/ on the phone. Log: ssh $CAM logread | grep webui"
fi
