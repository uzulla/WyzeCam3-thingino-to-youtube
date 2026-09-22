#!/bin/sh
# install.sh - push the YouTube Live relay to a Thingino camera over SSH.
# Run on the PC, from anywhere:
#   device/install.sh root@<camera-ip> [path/to/ffmpeg]
#
# Safe to re-run. Meant to be run on a freshly installed (or freshly updated)
# Thingino: reinstalling/updating the firmware removes everything placed here.
#
# The ffmpeg argument is optional; without it only scripts are (re)installed.
# An existing /etc/youtube-relay.json on the camera is never overwritten.
#
# Scope: this places OUR files (ffmpeg, supervisor, init script, config, the
# web UI page and its CGI). The one Thingino file it edits is
# /var/www/a/plugins.js (one line, the menu entry for the page); it never
# touches Thingino's settings. Updating or backing up the OS is out of scope.

set -e

CAM=$1
FFMPEG=$2
HERE=$(cd "$(dirname "$0")" && pwd)

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip> [path/to/ffmpeg]" >&2
	exit 1
fi
if [ -n "$FFMPEG" ] && [ ! -f "$FFMPEG" ]; then
	echo "ffmpeg binary not found: $FFMPEG" >&2
	exit 1
fi

# md5sum on Linux, md5 on macOS
local_md5() {
	if command -v md5sum >/dev/null 2>&1; then
		md5sum <"$1" | cut -d' ' -f1
	else
		md5 -q "$1"
	fi
}

. "$HERE/common.sh"
check_camera "$CAM"

if [ -n "$FFMPEG" ]; then
	echo "Installing ffmpeg"
	ssh "$CAM" 'df -h /overlay | tail -1'
	push "$FFMPEG" /usr/bin/ffmpeg 755
	want=$(local_md5 "$FFMPEG")
	got=$(ssh "$CAM" 'md5sum </usr/bin/ffmpeg' | cut -d' ' -f1)
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		echo "md5 mismatch after transfer (overlay full?)" >&2
		exit 1
	fi
	if ! ssh "$CAM" '/usr/bin/ffmpeg -hide_banner -protocols 2>/dev/null' |
		awk '$0 == "Output:" { out = 1; next } out && $1 == "rtmps" { found = 1 } END { exit !found }'; then
		echo "WARNING: installed ffmpeg does not run or lacks rtmps" >&2
		echo "         (toolchain/mbedTLS mismatch with this firmware? see README)" >&2
	fi
fi

echo "Installing scripts"
# TERM only asks the supervisor to stop; it exits after its current sleep and after
# reaping ffmpeg. Wait for that, or the new instance would briefly publish to
# the same stream key alongside the old one.
ssh "$CAM" '
	# Only trust the pidfile if that PID really is the supervisor (PIDs get reused)
	is_relay() { [ -n "$1" ] && grep -q youtube-relay "/proc/$1/cmdline" 2>/dev/null; }
	pid=$(cat /run/youtube-relay.pid 2>/dev/null)
	is_relay "$pid" || pid=""
	[ -n "$pid" ] && kill "$pid" 2>/dev/null
	rm -f /run/youtube-relay.pid
	i=0
	while is_relay "$pid" && [ $i -lt 45 ]; do
		i=$((i + 1))
		sleep 1
	done
	if is_relay "$pid"; then
		echo "old supervisor (pid $pid) did not exit, killing it" >&2
		pkill -P "$pid" 2>/dev/null # its ffmpeg child first, or it would be orphaned
		kill -9 "$pid" 2>/dev/null
	fi
	true
'
push "$HERE/youtube-relay" /usr/sbin/youtube-relay 755
push "$HERE/S93youtube-relay" /etc/init.d/S93youtube-relay 755

if ssh "$CAM" '[ -f /etc/youtube-relay.json ]'; then
	echo "Keeping existing /etc/youtube-relay.json"
else
	push "$HERE/youtube-relay.json.example" /etc/youtube-relay.json 600
	echo "  -> edit stream_key in /etc/youtube-relay.json (or use the SD card instead)"
fi

ssh "$CAM" '/etc/init.d/S93youtube-relay start'

# Web UI page (docs/relay.md, "Web UI"): Services > YouTube Live. plugins.js is
# generated at firmware build time; our entry is inserted right after
# "cfg.plugins = {" when it is not there yet (re-runs are no-ops). The entry
# travels as a file and goes in with "sed r": no quoting through ssh.
push "$HERE/www/youtube.html" /var/www/youtube.html 644
push "$HERE/www/x/json-youtube.cgi" /var/www/x/json-youtube.cgi 755
ssh "$CAM" 'cat > /tmp/youtube-nav.frag' <<'EOF'
  "youtube-relay": {"label": "YouTube Live relay", "name": "youtube-relay", "nav": [{"section": "ddServices", "position": "prepend", "items": [{"href": "/youtube.html", "label": "YouTube Live"}]}]},
EOF
ssh "$CAM" 'f=/var/www/a/plugins.js
	if ! grep -q "\"/youtube.html\"" $f && grep -q "^  cfg.plugins = {$" $f; then
		if sed "/^  cfg.plugins = {$/r /tmp/youtube-nav.frag" $f > $f.new && chmod 644 $f.new && mv $f.new $f; then
			echo "  menu: Services > YouTube Live -> /youtube.html"
		else
			rm -f $f.new
			echo "  warning: editing $f failed" >&2
		fi
	fi
	rm -f /tmp/youtube-nav.frag
	chmod 644 $f # an edit by an older version may have left it 0600, which uhttpd refuses to serve
	grep -q "\"/youtube.html\"" $f || echo "  warning: could not add /youtube.html to the menu (open it by URL)"'
echo "Web UI: Services > YouTube Live (http://${CAM#*@}/youtube.html)"
echo "Done. Logs: ssh $CAM 'logread | grep youtube-relay'"
