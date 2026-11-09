#!/bin/sh
# set-camera-name.sh - give the camera the project's name (docs/lapcam.md):
#   device/set-camera-name.sh root@<camera-ip> [name]      (name: lapcam)
#
# Three things Thingino derives from its own name, changed to the new one:
#   - the hostname (default "ing-wyze-cam3-<4 hex>"): hostname, /etc/hostname,
#     /etc/hosts. S04hostname keeps a custom name at boot. On the LAN the
#     camera is then <name>.local (mDNS)
#   - an mDNS service "_<name>._tcp" (/etc/mdns.d/<name>.service, next to
#     Thingino's own _thingino._tcp which stays): tools/find-camera looks for it
#   - the recordings folder: the recorder's default path is "%hostname", so
#     the SD card would get a folder named like the lapcam binary next to it.
#     The path is set to the fixed folder "recordings", and the old
#     "<old hostname>" folder is moved there (not while a recording runs)
# All of it lives in /etc (overlay) and is undone by a factory reset. Safe to
# re-run. Undo by hand:
#   ssh root@<camera-ip> 'hostname ing-wyze-cam3-XXXX; echo ing-wyze-cam3-XXXX >/etc/hostname;
#     sed -i "/^127.0.1.1/c127.0.1.1\ting-wyze-cam3-XXXX" /etc/hosts; rm /etc/mdns.d/lapcam.service;
#     jct /etc/prudynt.json set recorder.device_path %hostname; /etc/init.d/S50mdnsd restart'

set -e

CAM=$1
NAME=${2:-lapcam}
HERE=$(cd "$(dirname "$0")" && pwd)
SD=/mnt/mmcblk0p1

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [name]" >&2
	exit 1
fi
case "$NAME" in
*[!a-z0-9-]* | '' | -* | *-) echo "name must be lowercase letters, digits and hyphens (a hostname): '$NAME'" >&2; exit 1 ;;
esac
if [ ${#NAME} -gt 32 ]; then
	echo "name is too long (32 at most)" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

echo "Setting the camera's name to $NAME"
ssh "$CAM" "set -e
NAME='$NAME'
old=\$(hostname)
# hostname, the way S04hostname does it (it keeps a custom name at boot)
if [ \"\$old\" != \"\$NAME\" ]; then
	hostname \"\$NAME\"
	echo \"\$NAME\" >/etc/hostname
	if grep -q '^127.0.1.1' /etc/hosts; then
		sed -i \"/^127.0.1.1/c127.0.1.1\t\$NAME\" /etc/hosts
	else
		printf '127.0.1.1\t%s\n' \"\$NAME\" >>/etc/hosts
	fi
	echo \"  hostname: \$old -> \$NAME\"
else
	echo \"  hostname already \$NAME\"
fi
# mDNS: our own service type, next to Thingino's _thingino._tcp
port=\$(sed -n 's/.*-listen *[^:]*:\([0-9]*\).*/\1/p' $SD/lapcam.args 2>/dev/null | head -n 1)
[ -n \"\$port\" ] || port=80
# (not sourced: os-release has a NAME= of its own that would clobber ours)
product=\$(sed -n 's/^IMAGE_ID=//p' /etc/os-release | tr -d '\"'); [ -n \"\$product\" ] || product=unknown
version=\$(sed -n 's/^BUILD_ID=//p' /etc/os-release | tr -d '\"' | cut -d, -f1)
printf '# lapcam (device/set-camera-name.sh)\ntype _%s._tcp\nport %s\ntxt product=%s\ntxt version=%s\n' \"\$NAME\" \"\$port\" \"\$product\" \"\$version\" >/etc/mdns.d/\$NAME.service.tmp
mv /etc/mdns.d/\$NAME.service.tmp /etc/mdns.d/\$NAME.service
sh /etc/init.d/S50mdnsd restart >/dev/null 2>&1 || true
echo \"  mDNS: _\$NAME._tcp on port \$port, host \$NAME.local\"
# recordings: a fixed folder instead of %hostname (which would now be the binary's name)
mount=\$(jct /etc/prudynt.json get recorder.mount 2>/dev/null | tr -d '\"'); [ -n \"\$mount\" ] && [ \"\$mount\" != null ] || mount=$SD
cur=\$(jct /etc/prudynt.json get recorder.device_path 2>/dev/null | tr -d '\"')
if [ \"\$cur\" != recordings ]; then
	jct /etc/prudynt.json set recorder.device_path recordings >/dev/null
	echo \"  recorder.device_path: \$cur -> recordings\"
fi
if [ -d \"\$mount/\$old\" ] && [ \"\$old\" != \"\$NAME\" ]; then
	if ls /run/prudynt/mp4ctl-ch*.active >/dev/null 2>&1; then
		echo \"  a recording is running: \$mount/\$old was left; move it to \$mount/recordings later\"
	elif [ -e \"\$mount/recordings\" ]; then
		echo \"  \$mount/recordings exists already: \$mount/\$old was left for you to merge\"
	else
		mv \"\$mount/\$old\" \"\$mount/recordings\" && sync
		echo \"  recordings moved: \$mount/\$old -> \$mount/recordings\"
	fi
fi
# programs that read the hostname once at start
for s in S95beacon S97lapcam; do [ -x /etc/init.d/\$s ] && sh /etc/init.d/\$s restart >/dev/null 2>&1 || true; done
" || { echo "failed on the camera; see above" >&2; exit 1; }
echo "Done. The camera is $NAME.local; find it with: go -C tools/find-camera run ."
