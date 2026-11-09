#!/bin/sh
# install-rec-button.sh - double-click the camera's button to start/stop SD
# recording (docs/recording.md). Run on the PC:
#   device/install-rec-button.sh root@<camera-ip>
#
# Puts /usr/sbin/rec-button on the overlay and changes Thingino's button
# config /etc/thingino-button.conf (kept as thingino-button.conf.orig):
#   - the short-press line "KEY_ENTER TIMED 0.1 tell %ip" is commented out
#     (rec-button runs "tell %ip" itself for a single click)
#   - "KEY_ENTER PRESS/RELEASE 0 /usr/sbin/rec-button ..." are appended
# Then restarts the button daemon. The 5 s (reboot) and 20 s (factory reset)
# holds are untouched. Safe to re-run. Undo:
#   ssh root@<camera-ip> 'mv /etc/thingino-button.conf.orig /etc/thingino-button.conf;
#     rm /usr/sbin/rec-button; /etc/init.d/S15thingino-button restart'

set -e

CAM=$1
HERE=$(cd "$(dirname "$0")" && pwd)
CONF=/etc/thingino-button.conf

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip>" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

if ! ssh "$CAM" "[ -f $CONF ] && [ -x /usr/bin/thingino-button ] && grep -q '^KEY_ENTER ' $CONF"; then
	echo "No thingino-button with a KEY_ENTER mapping on the camera - nothing was changed" >&2
	exit 1
fi

echo "Installing rec-button"
push "$HERE/rec-button" /usr/sbin/rec-button 755
ssh "$CAM" "set -e
[ -f $CONF.orig ] || cp $CONF $CONF.orig
sed -i 's|^KEY_ENTER TIMED 0.1 tell %ip\$|#&   # moved to rec-button (single click)|' $CONF
if ! grep -q '/usr/sbin/rec-button' $CONF; then
	printf '\n# rec-button: double click starts/stops SD recording, single click tells the IP\nKEY_ENTER PRESS 0 /usr/sbin/rec-button press\nKEY_ENTER RELEASE 0 /usr/sbin/rec-button release\n' >>$CONF
fi
# the init script's stop can miss the daemon started at boot, which would leave
# two daemons running every mapping twice: make sure none is left before starting
/etc/init.d/S15thingino-button stop >/dev/null 2>&1 || true
killall thingino-button 2>/dev/null || true
sleep 0.5
/etc/init.d/S15thingino-button start >/dev/null
sleep 1
[ \"\$(pidof thingino-button | wc -w)\" = 1 ]"
echo "Done. Double-click the button to start/stop recording. Log: ssh $CAM 'logread | grep rec-button'"
