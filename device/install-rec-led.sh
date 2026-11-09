#!/bin/sh
# install-rec-led.sh - red LED on while the camera records to the SD card
# (docs/recording.md). Run on the PC:
#   device/install-rec-led.sh root@<camera-ip>
#
# Puts the watcher script and its init script on the overlay (two small files)
# and starts it. Safe to re-run. Undo:
#   ssh root@<camera-ip> '/etc/init.d/S96rec-led stop; rm /etc/init.d/S96rec-led /usr/sbin/rec-led'

set -e

CAM=$1
HERE=$(cd "$(dirname "$0")" && pwd)

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip>" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

if ! ssh "$CAM" '[ -d /sys/class/leds/led_r ]'; then
	echo "No red LED channel (/sys/class/leds/led_r) on the camera - nothing was changed" >&2
	exit 1
fi

echo "Installing rec-led"
if ! ssh "$CAM" '[ -x /etc/init.d/S96rec-led ] || exit 0; /etc/init.d/S96rec-led stop'; then
	echo "the running rec-led did not stop - nothing was changed" >&2
	exit 1
fi
push "$HERE/rec-led" /usr/sbin/rec-led 755
push "$HERE/S96rec-led" /etc/init.d/S96rec-led 755
ssh "$CAM" '/etc/init.d/S96rec-led start'
echo "Done. Log: ssh $CAM 'logread | grep rec-led'"
