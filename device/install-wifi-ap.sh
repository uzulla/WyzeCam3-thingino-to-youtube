#!/bin/sh
# install-wifi-ap.sh - hold the camera's button for 5 seconds to turn it into
# an open Wi-Fi access point "Wyze-cam" until the next reboot (docs/lapcam.md).
# Run on the PC:
#   device/install-wifi-ap.sh root@<camera-ip>
#
# Puts /usr/sbin/wifi-ap on the overlay and changes one line of Thingino's
# button config /etc/thingino-button.conf (kept as thingino-button.conf.orig
# if not already): the 5-second hold, which Thingino maps to "wipe the Wi-Fi
# settings and reboot", now runs wifi-ap instead (the saved networks stay).
# The 20-second hold (factory reset) is untouched. Safe to re-run. Undo:
#   ssh root@<camera-ip> 'sed -i "s|^KEY_ENTER TIMED 5 /usr/sbin/wifi-ap.*|KEY_ENTER TIMED 5 /sbin/wlan reset;reboot|" /etc/thingino-button.conf;
#     rm /usr/sbin/wifi-ap; /etc/init.d/S15thingino-button restart'

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

# only the line Thingino ships (or ours from an earlier run) may be there:
# another 5-second mapping would be left alone by the sed below and then the
# final check would fail after the files were already changed
if ! ssh "$CAM" "[ -f $CONF ] && [ -x /etc/init.d/S38wpa_supplicant ] && grep -q -E '^KEY_ENTER TIMED 5 (/sbin/wlan reset;reboot|/usr/sbin/wifi-ap)\$' $CONF && ! grep -E '^KEY_ENTER TIMED 5 ' $CONF | grep -v -q -E '(/sbin/wlan reset;reboot|/usr/sbin/wifi-ap)\$'"; then
	echo "The camera's 5-second button mapping is not the one this installer knows (Thingino's 'wlan reset;reboot'); look at $CONF first - nothing was changed" >&2
	exit 1
fi

echo "Installing wifi-ap"
push "$HERE/wifi-ap" /usr/sbin/wifi-ap 755
ssh "$CAM" "set -e
[ -f $CONF.orig ] || cp $CONF $CONF.orig
sed -i 's|^KEY_ENTER TIMED 5 /sbin/wlan reset;reboot\$|#&   # replaced by wifi-ap (install-wifi-ap.sh)\nKEY_ENTER TIMED 5 /usr/sbin/wifi-ap|' $CONF
/etc/init.d/S15thingino-button stop >/dev/null 2>&1 || true
killall thingino-button 2>/dev/null || true
sleep 0.5
/etc/init.d/S15thingino-button start >/dev/null
sleep 1
[ \"\$(pidof thingino-button | wc -w)\" = 1 ]
grep -q '^KEY_ENTER TIMED 5 /usr/sbin/wifi-ap' $CONF"
echo "Done. Hold the button 5 s (a chime sounds), release: the camera becomes the open access point 'Wyze-cam'."
echo "Then open http://100.64.1.1/ on the phone (with the port from lapcam.args, if any). Reboot to return to the saved Wi-Fi. Log: ssh $CAM 'logread | grep wifi-ap'"
