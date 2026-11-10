#!/bin/sh
# install-keys-from-sd.sh - optional add-on: let the camera take its SSH
# public keys (authorized_keys) from the SD card at boot, so they survive a
# firmware flash from the card (docs/sdcard.md). Run on the PC:
#   device/install-keys-from-sd.sh root@<camera-ip> [path/to/key.pub]
#
# Puts the boot script on the overlay and, when a public key file is given,
# ADDS its keys to the card's authorized_keys (keys already there stay; the
# file may hold several keys, one per line; each must be one ssh-keygen can
# read) and applies the result right away. Without a key file nothing changes
# until an authorized_keys is on the card and the camera reboots.
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
if [ -n "$KEY" ]; then
	n=$(check_pubkeys "$KEY")
fi
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
# merge: the card's keys plus the new ones, each once, comments dropped
echo "  $KEY ($n key(s)) -> $SD/authorized_keys (merged with what is there)"
tr -d '\r' <"$KEY" | ssh "$CAM" "cat '$SD/authorized_keys' - 2>/dev/null | tr -d '\r' | grep -v -E '^[[:space:]]*(#|\$)' | awk '!seen[\$0]++' > '$SD/authorized_keys.new' && mv -f '$SD/authorized_keys.new' '$SD/authorized_keys' && sync" || { echo "could not write $SD/authorized_keys on the camera" >&2; exit 1; }
ssh "$CAM" "sh /etc/init.d/S37keys-from-sd start; echo \"  keys on the card and on the camera now: \$(grep -c -v '^#' $SD/authorized_keys) / \$(grep -c -v '^#' /root/.ssh/authorized_keys)\""
echo "Installed. The card's authorized_keys is applied at every boot (it replaces the camera's when they differ)."
