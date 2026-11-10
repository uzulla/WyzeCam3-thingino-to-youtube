#!/bin/sh
# prepare.sh - get a Thingino source tree ready to build the lapcam firmware
# image (docs/sdcard.md). Run on the build host, from anywhere:
#   firmware/prepare.sh <thingino-firmware dir> [camera]
#     camera: wyze_cam3_t31x_gc2053_atbm6031 (the default), or another Thingino
#             camera name (configs/cameras/<camera>/ in the tree)
#
# Two things, both safe to re-run (firmware/build.sh runs this first):
#
#  1. Our patches go into the tree - the same ones docs/build.md applies by
#     hand: RTMPS in ffmpeg (patches/thingino-ffmpeg-rtmps.diff on the .mk and
#     the RTMP chunk-size patch), the text/image OSD and the msgChannel warning
#     fix in prudynt, the record-argument fix of the agent adapter - and one
#     more for the image: the setup portal's Wi-Fi network is named like the
#     camera (lapcam-xxxx), not THINGINO-xxxx (patches/thingino-wifi-portal-ssid.diff).
#
#  2. user/<camera>/ is written - Thingino's "user layer", baked into the image
#     by its Makefile (see docs/build/local-build-settings.md in the tree):
#       local.fragment  8x8 OSD font (full ASCII) in prudynt
#       prudynt.json    merged into the image's /etc/prudynt.json: stream0 at
#                       720p 10 fps 1 Mbps (what docs/recording.md measured),
#                       OSD pool for the text/image layers, recorder on the SD
#                       card in "recordings", JPEG channel 5 fps
#       thingino.json   merged into /etc/thingino.json: netwatch off (it
#                       reboots the OS when the gateway stops answering ping,
#                       which is worse than the outage for a stream; docs/netwatch.md),
#                       day/night switching off, held at day
#       local.uenv.txt  U-Boot env: red LED on while the bootloader runs, so a
#                       flash from the SD card (1-2 minutes) is visible
#       overlay/        the data partition's first content, i.e. what the
#                       device/install-*.sh scripts put on a camera by hand:
#                       init scripts (S37wifi-from-sd S37keys-from-sd S93osd-config
#                       S93youtube-relay S94osd-feed S95beacon S96rec-led
#                       S97lapcam), the helper scripts in /usr/sbin, the button
#                       config (double click = record, 5 s = Wi-Fi AP), the
#                       agent adapter with the record fix, hostname lapcam-xxxx
#                       (Thingino's S04hostname with "lapcam" as the default
#                       name), the _lapcam._tcp mDNS service, timezone
#     overlay/ is wiped and regenerated. The other files are overwritten; a
#     local.mk or opt/ of your own in user/<camera>/ is left alone.
#
# Not in the image (the flash is 16 MB: the rootfs has about 50 KB to spare,
# the data partition 8 MB): the Go programs (lapcam 9 MB, osd-feed 9.5 MB,
# beacon 4 MB) and ffmpeg (2.5 MB) stay on the SD card, where the init scripts
# look for them anyway (firmware/make-sdcard.sh puts them there).
#
# Timezone: LAPCAM_TIMEZONE (default Asia/Tokyo) and LAPCAM_TZ (its POSIX form,
# default JST-9) in the environment; both must change together.

set -e
umask 022 # the overlay's modes end up on the camera

TREE=$1
CAMERA=${2:-wyze_cam3_t31x_gc2053_atbm6031}
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=$(cd "$HERE/.." && pwd)
NAME=lapcam
TIMEZONE=${LAPCAM_TIMEZONE:-Asia/Tokyo}
TZ_POSIX=${LAPCAM_TZ:-JST-9}

if [ -z "$TREE" ] || [ ! -f "$TREE/Makefile" ] || [ ! -d "$TREE/package/prudynt-t" ]; then
	echo "Usage: $0 <thingino-firmware dir> [camera]" >&2
	echo "(the directory must be a Thingino source tree; docs/build.md tells how to get one)" >&2
	exit 1
fi
case "$CAMERA" in
"" | *[!A-Za-z0-9_-]*) echo "not a camera name: '$CAMERA'" >&2; exit 1 ;;
esac
if [ ! -d "$TREE/configs/cameras/$CAMERA" ]; then
	echo "no such camera in the tree: configs/cameras/$CAMERA" >&2
	exit 1
fi
EXPECT_HASH=da40db6 # the commit the camera's firmware (BUILD_ID ciao+da40db6) was built from
TREE=$(cd "$TREE" && pwd)

# --- 1. patches --------------------------------------------------------------

# apply_diff <diff> <package>: a git diff against tracked files of the tree.
# Applied once; "already applied" is detected by checking the reverse. A newly
# applied diff changes <package>'s files, which Buildroot would not notice on
# an incremental build: the package is noted in user/<camera>/.rebuild for
# build.sh (make rebuild-<package>).
apply_diff() {
	if git -C "$TREE" apply --check -R "$1" 2>/dev/null; then
		echo "  already applied: $(basename "$1")"
		return
	fi
	if ! git -C "$TREE" apply --check "$1" 2>/dev/null; then
		# an earlier version of this patch may be in the tree: the files it
		# touches are ours to restore (they are tracked, so nothing is lost)
		for f in $(sed -n 's|^+++ b/||p' "$1"); do
			if ! git -C "$TREE" diff --quiet -- "$f" 2>/dev/null; then
				echo "  restoring $f (an older version of $(basename "$1") was in it)"
				git -C "$TREE" checkout -q -- "$f"
			fi
		done
	fi
	if git -C "$TREE" apply --check "$1" 2>/dev/null; then
		git -C "$TREE" apply "$1"
		echo "  applied: $(basename "$1") (rebuilds $2)"
		mkdir -p "$TREE/user/$CAMERA"
		echo "$2" >>"$TREE/user/$CAMERA/.rebuild"
	else
		echo "patch does not apply (tree changed?): $1" >&2
		git -C "$TREE" apply --check "$1" || true
		exit 1
	fi
}

# put_patch <diff> <package dir/name.patch> <package>: Buildroot applies every
# *.patch in a package's directory to that package's source, in name order,
# when it extracts the package: a new or changed patch needs the package
# rebuilt from scratch (noted for build.sh like apply_diff does).
put_patch() {
	if [ -f "$TREE/$2" ] && cmp -s "$1" "$TREE/$2"; then
		echo "  in place: $2"
	else
		cp "$1" "$TREE/$2"
		echo "  copied: $2 (rebuilds $3)"
		mkdir -p "$TREE/user/$CAMERA"
		echo "$3" >>"$TREE/user/$CAMERA/.rebuild"
	fi
}

echo "Patches in $TREE"
apply_diff "$REPO/patches/thingino-ffmpeg-rtmps.diff" thingino-ffmpeg
put_patch "$REPO/patches/thingino-ffmpeg-rtmp-chunk-size.diff" package/thingino-ffmpeg/0002-rtmp-chunk-size.patch thingino-ffmpeg
put_patch "$REPO/patches/prudynt-osd-textfile.diff" package/prudynt-t/0001-osd-textfile.patch prudynt-t
put_patch "$REPO/patches/prudynt-msgchannel-warning.diff" package/prudynt-t/0002-msgchannel-warning.patch prudynt-t
apply_diff "$REPO/patches/thingino-agent-adapter-record-args.diff" prudynt-t
# the setup portal's access point (a camera without Wi-Fi settings opens one
# at boot) is named like the camera (its hostname, lapcam-xxxx) instead of
# THINGINO-<4 hex of the MAC>
apply_diff "$REPO/patches/thingino-wifi-portal-ssid.diff" wifi

# The image's BUILD_ID is "<branch>+<hash>" (e.g. ciao+da40db6); device/common.sh
# accepts only the value the camera shipped with. A detached checkout would give
# "HEAD+da40db6", so the branch has to be named ciao.
branch=$(git -C "$TREE" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
hash=$(git -C "$TREE" rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
if [ "$hash" != "$EXPECT_HASH" ]; then
	echo "WARNING: the tree is at $hash, not $EXPECT_HASH: BUILD_ID will differ from the camera's, device/common.sh will refuse the image, and the patches may not fit" >&2
fi
if [ "$branch" != "ciao" ]; then
	# only a 'ciao' that does not exist yet, or already points here, is
	# touched: never move somebody's branch
	if git -C "$TREE" rev-parse -q --verify refs/heads/ciao >/dev/null 2>&1 &&
		[ "$(git -C "$TREE" rev-parse refs/heads/ciao)" != "$(git -C "$TREE" rev-parse HEAD)" ]; then
		echo "the tree is on '$branch' and a branch 'ciao' exists at another commit; the build needs HEAD on a branch named ciao (BUILD_ID is branch+hash). Move or delete that branch first" >&2
		exit 1
	fi
	echo "  the tree is on '$branch' (BUILD_ID would be $branch+$hash): putting a branch 'ciao' on this commit"
	git -C "$TREE" checkout -q -B ciao
	branch=ciao
fi
echo "  BUILD_ID will be $branch+$hash"

# --- 2. user/<camera>/ -------------------------------------------------------

U=$TREE/user/$CAMERA
O=$U/overlay
echo "User layer $U"
rm -rf "$O"
mkdir -p "$O/etc/init.d" "$O/etc/mdns.d" "$O/usr/sbin"

# install <src> <dst-in-overlay> <mode>
put() {
	cp "$1" "$O$2"
	chmod "$3" "$O$2"
}

cat >"$U/local.fragment" <<'EOF'
# lapcam (firmware/prepare.sh): full-ASCII 8x8 font for the text OSD
BR2_PACKAGE_PRUDYNT_T_OSD_FONT_8X8=y
EOF

# prudynt.json is merged into /etc/prudynt.json when the prudynt-t package is
# installed, so a change here needs that package rebuilt (thingino.json is
# merged at the end of every build, and the overlay is packed every time)
cat >"$U/prudynt.json.new" <<'EOF'
{
  "general": {
    "osd_pool_size": 2048
  },
  "recorder": {
    "device_path": "recordings",
    "mount": "/mnt/mmcblk0p1"
  },
  "stream0": {
    "bitrate": 1024,
    "fps": 10,
    "gop": 20,
    "height": 720,
    "max_gop": 30,
    "width": 1280
  },
  "stream2": {
    "fps": 5,
    "jpeg_quality": 60
  }
}
EOF
if [ -f "$U/prudynt.json" ] && cmp -s "$U/prudynt.json" "$U/prudynt.json.new"; then
	rm -f "$U/prudynt.json.new"
else
	if [ -f "$U/prudynt.json" ]; then
		echo "  prudynt.json changed (rebuilds prudynt-t)"
		echo prudynt-t >>"$U/.rebuild"
	fi
	mv -f "$U/prudynt.json.new" "$U/prudynt.json"
fi

# daynight: Thingino's day/night switching (IR cut filter, IR LEDs, black and
# white at night) is for surveillance; in a car the IR only lights up the
# windscreen and the switch changes the picture mid-stream in tunnels and
# shade. Disabled and held at day; daynightd itself keeps running idle (its
# init script also creates the API key prudynt and the agent need)
cat >"$U/thingino.json" <<'EOF'
{
  "daynight": {
    "enabled": false,
    "force_mode": "day"
  },
  "netwatch": {
    "enabled": false
  }
}
EOF

for s in S37wifi-from-sd S37keys-from-sd S93osd-config S93youtube-relay S94osd-feed S95beacon S96rec-led S97lapcam; do
	put "$REPO/device/$s" /etc/init.d/$s 755
done
for s in osd-config youtube-relay rec-led rec-button wifi-ap; do
	put "$REPO/device/$s" /usr/sbin/$s 755
done

# the agent's backend adapter (the Rec button of Thingino's UI and lapcam's
# recording go through it): the build installs prudynt's adapter and then
# thingino-agent's "null" one on top of it, so the image has the null adapter
# like the shipped firmware (docs/recording.md). The overlay puts the prudynt
# one with the record-argument fix in place, as install-agent-adapter.sh does
mkdir -p "$O/usr/libexec/agent"
put "$REPO/device/agent-adapter" /usr/libexec/agent/adapter.sh 644

# button config: Thingino's file with the two edits install-rec-button.sh and
# install-wifi-ap.sh make on a camera (comments mark them)
src=$TREE/package/thingino-button/files/thingino-button.conf
dst=$O/etc/thingino-button.conf
awk '
/^KEY_ENTER TIMED 0\.1 tell %ip$/ { print "#" $0 "   # moved to rec-button (single click)"; next }
/^KEY_ENTER TIMED 5 \/sbin\/wlan reset;reboot$/ { print "#" $0 "   # replaced by wifi-ap"; print "KEY_ENTER TIMED 5 /usr/sbin/wifi-ap"; next }
{ print }' "$src" >"$dst"
printf '\n# rec-button: double click starts/stops SD recording, single click tells the IP\nKEY_ENTER PRESS 0 /usr/sbin/rec-button press\nKEY_ENTER RELEASE 0 /usr/sbin/rec-button release\n' >>"$dst"
if ! grep -q '^#KEY_ENTER TIMED 0\.1 tell %ip' "$dst" || ! grep -q '^KEY_ENTER TIMED 5 /usr/sbin/wifi-ap$' "$dst" || grep -q '^KEY_ENTER TIMED 5 /sbin/wlan' "$dst"; then
	echo "Thingino's thingino-button.conf no longer has the lines this script edits: $src" >&2
	exit 1
fi
chmod 644 "$dst"

# hostname: lapcam-<4 hex>, the 4 hex being the end of the base MAC address
# the camera derives from its SoC (two cameras on one network then have
# different names; the Wi-Fi MAC itself is that address plus one, so the 4
# hex are not the Wi-Fi MAC's). The setup portal's and wifi-ap's networks
# take the hostname as their name, so the phone sees the same lapcam-xxxx.
# Thingino's S04hostname does exactly this ("<default>-<suffix>" when the
# name is the default or an earlier auto-generated one, a custom name kept),
# with the default taken from /etc/os-release, which the build sets to
# ing-wyze-cam3. The overlay carries Thingino's script with that one function
# answering "lapcam" instead; a name set by hand (device/set-camera-name.sh)
# is kept as before. /etc/hostname starts as the default name itself (the
# rootfs has ing-wyze-cam3 there, which the script would take for a custom
# name and keep); /etc/hosts is left to it.
echo "$NAME" >"$O/etc/hostname"
chmod 644 "$O/etc/hostname"
src=$TREE/overlay/etc/init.d/S04hostname
dst=$O/etc/init.d/S04hostname
if ! grep -q 'sed -n .s/^HOSTNAME=//p. \$OS_RELEASE_FILE' "$src"; then
	echo "Thingino's S04hostname no longer reads the default name the way this script expects: $src" >&2
	exit 1
fi
{
	echo "#!/bin/sh"
	echo "# lapcam (firmware/prepare.sh): Thingino's S04hostname with the default name"
	echo "# \"$NAME\" instead of the one in /etc/os-release, so cameras are named $NAME-<4 hex>"
	sed -e '1{/^#!/d;}' -e "s|echo \"\$(sed -n 's/^HOSTNAME=//p' \$OS_RELEASE_FILE)\"|echo $NAME|" "$src"
} >"$dst"
chmod 755 "$dst"
if ! grep -q "echo $NAME\$" "$dst"; then
	echo "could not put the default name into $dst" >&2
	exit 1
fi

# the release this image belongs to (docs/release.md): lapcam reads it and
# compares it with its own version; tools/find-camera sees it in mDNS
VERSION=$(sh "$REPO/tools/version.sh")
cat >"$O/etc/lapcam-release" <<EOF
VERSION=$VERSION
COMMIT=$(git -C "$REPO" rev-parse --short=7 HEAD 2>/dev/null || echo unknown)
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%MZ)
THINGINO=$branch+$hash
CAMERA=$CAMERA
EOF
chmod 644 "$O/etc/lapcam-release"

# mDNS: _lapcam._tcp next to Thingino's own services (tools/find-camera looks for it)
cat >"$O/etc/mdns.d/$NAME.service" <<EOF
# lapcam (firmware/prepare.sh)
type _${NAME}._tcp
port 80
txt product=$CAMERA
txt version=$VERSION
txt thingino=$branch+$hash
EOF

# U-Boot: red LED on while the bootloader runs. Thingino's U-Boot applies the
# env's gpio_* lists at start (gpio_default sets 38O = red off; gpio_user comes
# right after it and is free for us): 38o drives the pin low, which is on for
# this active-low LED. The kernel's LED driver turns it off again when it
# probes, so solid red means "in U-Boot": 2 seconds on a normal boot, the
# whole 1-2 minutes while autoupdate-full.bin is written from the SD card.
# Thingino's blue blink (S00blink) follows while the init scripts run.
cat >"$U/local.uenv.txt" <<'EOF2'
# lapcam (firmware/prepare.sh): red LED on while U-Boot runs (see the script)
gpio_user=38o
EOF2

# timezone, the way Thingino's own UI stores a user's choice
echo "$TIMEZONE" >"$O/etc/timezone"
echo "$TZ_POSIX" >"$O/etc/TZ"
echo user >"$O/etc/timezone.source"

echo "  $(find "$O" -type f | wc -l | tr -d ' ') overlay files, local.fragment, local.uenv.txt, prudynt.json, thingino.json; lapcam version $VERSION"
echo "Done. Build with: firmware/build.sh $TREE $CAMERA"
