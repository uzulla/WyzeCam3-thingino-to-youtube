#!/bin/sh
# install-webui.sh - put webui (docs/webui.md) on the camera, in place of
# Thingino's web UI. Run on the PC, after device/webui/build.sh:
#   device/install-webui.sh root@<camera-ip> [dist/webui]
#
# The binary (about 9 MB) goes to the SD card, which must be mounted at
# /mnt/mmcblk0p1; only the small init script goes to the overlay. Unless the
# card already has a webui.args, one is written with "-listen :80" and
# Thingino's web server uhttpd (ports 80/443) is stopped and disabled at boot:
# the app then answers on http://<camera-ip>/. To keep Thingino's UI and run
# the app on port 8000 instead, put "-listen :8000" in webui.args first.
# webui is (re)started at the end. Undo (Thingino's UI comes back):
#   ssh root@<camera-ip> '/etc/init.d/S97webui stop; rm /etc/init.d/S97webui /mnt/mmcblk0p1/webui /mnt/mmcblk0p1/webui.args;
#     service enable uhttpd; /etc/init.d/S60uhttpd start'

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

echo "Installing webui"
if ! ssh "$CAM" '[ -x /etc/init.d/S97webui ] || exit 0; /etc/init.d/S97webui stop'; then
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
	echo "Taking over port 80 from Thingino's web UI (uhttpd stopped and disabled at boot)"
	ssh "$CAM" "printf -- '-listen :80\n' > $SD/webui.args"
fi
port=$(ssh "$CAM" "head -n 1 $SD/webui.args | sed -n 's/.*-listen *[^:]*:\([0-9]*\).*/\1/p'")
if [ "${port:-8000}" = 80 ]; then
	ssh "$CAM" '[ -f /etc/init.d/S60uhttpd ] || exit 0; sh /etc/init.d/S60uhttpd stop >/dev/null 2>&1 || true; service disable uhttpd >/dev/null 2>&1 || chmod -x /etc/init.d/S60uhttpd; i=0; while netstat -lnt 2>/dev/null | grep -q ":80 " && [ $i -lt 10 ]; do sleep 1; i=$((i + 1)); done'
fi
ssh "$CAM" '/etc/init.d/S97webui start'
if [ "${port:-8000}" = 80 ]; then
	echo "Done. Open http://<camera-ip>/ on the phone. Log: ssh $CAM logread | grep webui"
else
	echo "Done. Open http://<camera-ip>:$port/ on the phone. Log: ssh $CAM logread | grep webui"
fi
