#!/bin/sh
# install-prudynt-osd.sh - install the prudynt build with the text-file OSD overlay.
# Run on the PC:
#   device/install-prudynt-osd.sh root@<camera-ip> path/to/prudynt
#
# OPTIONAL, and separate from install.sh because it replaces a Thingino program
# (the streamer) at run time. Thingino's own /usr/bin/prudynt is NOT overwritten:
# the new binary is stored as /usr/bin/prudynt-osd and S30prudynt-osd bind-mounts
# it over /usr/bin/prudynt at boot. Also adds the "OSD text" page to Thingino's web
# UI (the Streamer menu entry that led to Thingino's own OSD page is pointed at it).
# Undo:
#   ssh root@<camera-ip> 'rm /etc/init.d/S30prudynt-osd /etc/init.d/S93osd-config /usr/bin/prudynt-osd /usr/bin/prudynt-osd.build /usr/sbin/osd-config /var/www/osd-text.html /var/www/x/json-osd-text.cgi; sed -i "s#/osd-text.html#/streamer-osd.html#; s#\"OSD text\"#\"OSD Elements\"#" /var/www/a/plugins.js /var/www/a/plugins/prudynt.webui.json; sed -i "s|\${DAEMON##\*/}|\$DAEMON|g" /etc/init.d/S31prudynt; reboot'
# (the S31prudynt edit may also just stay: it is harmless with the stock prudynt)
#
# prudynt is restarted at the end, which interrupts the stream for a few seconds
# (the youtube-relay supervisor reconnects by itself).

set -e

CAM=$1
BIN=$2
HERE=$(cd "$(dirname "$0")" && pwd)

if [ -z "$CAM" ] || [ -z "$BIN" ]; then
	echo "Usage: $0 root@<camera-ip> path/to/prudynt" >&2
	exit 1
fi
if [ ! -f "$BIN" ]; then
	echo "prudynt binary not found: $BIN" >&2
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

echo "Installing prudynt-osd"
ssh "$CAM" 'df -h /overlay | tail -1'
# Upload next to the final name first: the slow part happens while the stream is
# still up, and the file prudynt is running from (bind-mounted over
# /usr/bin/prudynt) is not touched while it is mounted. Replacing a mounted file
# leaves a stale "(deleted)" mount behind.
push "$BIN" /usr/bin/prudynt-osd.upload 755
want=$(local_md5 "$BIN")
got=$(ssh "$CAM" 'md5sum </usr/bin/prudynt-osd.upload' | cut -d' ' -f1)
if [ -z "$want" ] || [ "$want" != "$got" ]; then
	ssh "$CAM" 'rm -f /usr/bin/prudynt-osd.upload' || true
	echo "md5 mismatch after transfer (overlay full?)" >&2
	exit 1
fi
ssh "$CAM" "echo '$SUPPORTED_BUILD' > /usr/bin/prudynt-osd.build"
push "$HERE/S30prudynt-osd" /etc/init.d/S30prudynt-osd 755
push "$HERE/osd-progress-demo" /usr/sbin/osd-progress-demo 755
# osd-config re-sends the OSD settings from prudynt-osd.json (SD card, then /etc)
# after every prudynt restart. It does nothing until such a file exists.
ssh "$CAM" '[ -x /etc/init.d/S93osd-config ] && /etc/init.d/S93osd-config stop >/dev/null 2>&1; true'
push "$HERE/osd-config" /usr/sbin/osd-config 755
push "$HERE/S93osd-config" /etc/init.d/S93osd-config 755

echo "Switching prudynt (the stream drops for a few seconds)"
# Failures here are not fatal on purpose: the check below decides, and rolls back.
# "S31prudynt stop" returns before prudynt is gone; starting too early makes the new
# one quit with "Another Prudynt instance appears to be running".
STOP_PRUDYNT='/etc/init.d/S31prudynt stop; n=0; while pidof prudynt >/dev/null && [ $n -lt 20 ]; do n=$((n + 1)); sleep 1; done'
# If prudynt cannot be stopped, the old binary stays mounted and running, and the
# check below would happily report success for it: give up instead.
if ! ssh "$CAM" "$STOP_PRUDYNT"'; ! pidof prudynt >/dev/null'; then
	ssh "$CAM" 'rm -f /usr/bin/prudynt-osd.upload; /etc/init.d/S31prudynt start; /etc/init.d/S93osd-config start' >/dev/null 2>&1 || true
	echo "prudynt did not stop (or the camera could not be reached) - nothing was switched" >&2
	exit 1
fi
ssh "$CAM" '
	/etc/init.d/S30prudynt-osd stop
	mv /usr/bin/prudynt-osd.upload /usr/bin/prudynt-osd
	/etc/init.d/S30prudynt-osd start && /etc/init.d/S31prudynt start' || true

# The patched build answers the osd.textfile query with its settings; stock prudynt returns {}.
i=0
ok=""
while [ $i -lt 15 ]; do
	case $(ssh "$CAM" "prudyntctl json '{\"osd\":{\"textfile\":null}}' 2>/dev/null" || true) in
	*'"textfile"'*)
		ok=1
		break
		;;
	esac
	i=$((i + 1))
	sleep 1
done
if [ -z "$ok" ]; then
	echo "prudynt did not come up with osd.textfile support - going back to the stock prudynt" >&2
	# osd-config is pointless (and would keep poking the stock prudynt) without the patch
	ssh "$CAM" 'chmod -x /etc/init.d/S93osd-config; '"$STOP_PRUDYNT"'; /etc/init.d/S30prudynt-osd stop; chmod -x /etc/init.d/S30prudynt-osd; /etc/init.d/S31prudynt start' || true
	exit 1
fi
ssh "$CAM" '/etc/init.d/S93osd-config start' || echo "warning: osd-config did not start (prudynt itself is fine)" >&2

# With the bind mount, /proc/<pid>/exe of the running prudynt reads
# /usr/bin/prudynt-osd, and Thingino's S31prudynt checks liveness with
# pidof "$DAEMON" where DAEMON=/usr/bin/prudynt: a full path, matched against
# exe, so it finds nothing. Its "stop" then returns at once and "restart"
# starts the new prudynt while the old one is still shutting down; the new one
# quits with "Another Prudynt instance appears to be running" and none is left.
# That breaks Thingino's own Restart streamer, its OSD page and resolution
# changes. Match by name instead (${DAEMON##*/} = prudynt): the same for the
# stock binary, so the edit is harmless without the bind mount. Re-runs are
# no-ops; a firmware update restores Thingino's file along with everything else.
ssh "$CAM" 'f=/etc/init.d/S31prudynt
	if grep -q "pidof \"\$DAEMON\"" $f; then
		sed -e "s|pidof \"\$DAEMON\"|pidof \"\${DAEMON##*/}\"|" \
			-e "s|killall \"\$DAEMON\"|killall \"\${DAEMON##*/}\"|" \
			-e "s|killall -9 \"\$DAEMON\"|killall -9 \"\${DAEMON##*/}\"|" $f > $f.new \
			&& sh -n $f.new && chmod 755 $f.new && mv $f.new $f \
			&& echo "  S31prudynt: liveness check by name (restart works with the bind mount)" \
			|| { rm -f $f.new; echo "  warning: could not edit $f - Thingino'"'"'s own prudynt restart may leave prudynt stopped" >&2; }
	elif ! grep -q "DAEMON##\*/" $f; then
		echo "  warning: $f does not look as expected (no pidof \"\$DAEMON\") - not edited; Thingino'"'"'s own prudynt restart may leave prudynt stopped" >&2
	fi'

# Web UI page (docs/osd.md), only now that the patched prudynt is known to run:
# a rollback above must not leave the menu pointing at a page for a feature the
# stock prudynt does not have. Thingino's own OSD page (streamer-osd.html) stays
# in place, but its menu entry is pointed at ours: the two would otherwise fight
# over the same settings, and the old page's Save writes /etc/prudynt.json.
# plugins.js is generated at firmware build time, so the edit is a plain
# substitution, done only when the old href is still there (re-runs are no-ops).
push "$HERE/www/osd-text.html" /var/www/osd-text.html 644
push "$HERE/www/x/json-osd-text.cgi" /var/www/x/json-osd-text.cgi 755
# Same change in the manifest plugins.js is generated from (thingino-pkg
# regenerates plugins.js from /var/www/a/plugins/*.webui.json), so a
# regeneration keeps the menu pointing at our page
ssh "$CAM" 'm=/var/www/a/plugins/prudynt.webui.json
	if [ -f $m ] && grep -q "\"/streamer-osd.html\"" $m; then
		sed -e "s#\(\"href\": *\)\"/streamer-osd.html\"#\1\"/osd-text.html\"#" \
			-e "s#\(\"label\": *\)\"OSD Elements\"#\1\"OSD text\"#" $m > $m.new && chmod 644 $m.new && mv $m.new $m
	fi'
ssh "$CAM" 'f=/var/www/a/plugins.js
	if grep -q "\"/streamer-osd.html\"" $f; then
		sed -e "s#\(\"href\": *\)\"/streamer-osd.html\"#\1\"/osd-text.html\"#" \
			-e "s#\(\"label\": *\)\"OSD Elements\"#\1\"OSD text\"#" $f > $f.new && chmod 644 $f.new && mv $f.new $f
		echo "  menu: Streamer > OSD Elements -> /osd-text.html"
	fi
	# Earlier versions left the edited file 0600 (ssh umask), which uhttpd then
	# refused to serve: fix that on re-runs too
	chmod 644 $f
	grep -q "\"/osd-text.html\"" $f || echo "  warning: could not add /osd-text.html to the menu (open it by URL)"'

echo "Done. Try it:  ssh $CAM osd-progress-demo 20"
echo "Web UI: Streamer > OSD text (http://${CAM#*@}/osd-text.html)"
echo "Settings that survive restarts: put prudynt-osd.json on the SD card (see docs/osd.md)"
