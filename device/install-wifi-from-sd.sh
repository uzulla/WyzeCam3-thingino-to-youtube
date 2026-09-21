#!/bin/sh
# install-wifi-from-sd.sh - optional add-on: let the camera take its Wi-Fi config
# (several networks allowed) from a wpa_supplicant.conf on the SD card at boot.
# Run on the PC:
#   device/install-wifi-from-sd.sh root@<camera-ip>
#
# This adds a boot script that rewrites a Thingino OS file (/etc/wpa_supplicant.conf),
# which is why it is NOT part of install.sh. See S37wifi-from-sd for the file
# format and README.md for the caveats. Remove with:
#   ssh root@<camera-ip> 'rm /etc/init.d/S37wifi-from-sd'

set -e

CAM=$1
HERE=$(cd "$(dirname "$0")" && pwd)
if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip>" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

push "$HERE/S37wifi-from-sd" /etc/init.d/S37wifi-from-sd 755
echo "Installed. Nothing changes until a wpa_supplicant.conf is on the SD card and the camera reboots."
