#!/bin/sh
# install-keys-from-sd.sh - optional add-on: let the camera take its SSH
# public keys (authorized_keys) from the SD card at boot, so they survive a
# firmware flash from the card (docs/sdcard.md). Run on the PC:
#   device/install-keys-from-sd.sh root@<camera-ip> [path/to/key.pub]
#
# Puts the boot script on the overlay and, when a public key file is given,
# copies it to the card as authorized_keys and applies it right away (the
# key file may hold several keys, one per line). Without a key file nothing
# changes until an authorized_keys is on the card and the camera reboots.
# The firmware made by firmware/prepare.sh has the script already; this is
# for a camera set up by hand. Remove with:
#   ssh root@<camera-ip> 'rm /etc/init.d/S37keys-from-sd /mnt/mmcblk0p1/authorized_keys'

set -e

CAM=$1
KEY=$2
HERE=$(cd "$(dirname "$0")" && pwd)
SD=/mnt/mmcblk0p1
if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/key.pub]" >&2
	exit 1
fi
if [ -n "$KEY" ] && [ ! -f "$KEY" ]; then
	echo "no such file: $KEY" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

push "$HERE/S37keys-from-sd" /etc/init.d/S37keys-from-sd 755
if [ -z "$KEY" ]; then
	echo "Installed. Nothing changes until an authorized_keys is on the SD card and the camera reboots."
	exit 0
fi
if ! ssh "$CAM" "grep -q ' $SD ' /proc/mounts"; then
	echo "no SD card mounted at $SD on the camera; the key was not copied" >&2
	exit 1
fi
push "$KEY" "$SD/authorized_keys" 644
ssh "$CAM" "sync; sh /etc/init.d/S37keys-from-sd start; grep -c -v '^#' /root/.ssh/authorized_keys" | sed 's/^/  keys on the camera now: /'
echo "Installed. The card's authorized_keys is applied at every boot (it replaces the camera's when they differ)."
