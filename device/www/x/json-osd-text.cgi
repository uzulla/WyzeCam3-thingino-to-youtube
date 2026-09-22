#!/bin/sh
# shellcheck disable=SC1091,SC3043
# json-osd-text.cgi - backend of /osd-text.html (Thingino web UI page for the
# text-file OSD overlay, docs/osd.md). Install to /var/www/x/json-osd-text.cgi.
# Runs under uhttpd as root, like Thingino's own CGIs; authentication is
# Thingino's: the web UI session cookie, or the API key from /etc/thingino-api.key
# as ?token=<key> (auth.sh also takes an X-API-Key header, but this uhttpd does
# not pass that header on to CGIs).
#
#   GET  ?action=status
#        -> {"config_path":..., "config":<prudynt-osd.json or null>, "config_error":...,
#            "pool_size":<general.osd_pool_size in /etc/prudynt.json>,
#            "live":<prudyntctl json osd.textfile/2/3/burnin>,
#            "texts":{"textfile":"...", ...}  (current overlay files, null = none),
#            "log":[...]}                     (last logread lines about textfile/osd-config)
#   POST ?action=text&slot=1|2|3   body: the text (plain, ASCII)
#        -> writes the slot's file (osd.textfileN.path) with the tmp + mv contract from
#           docs/osd.md; an empty body removes the file (hides the overlay). This is
#           the HTTP way of doing what osd-progress-demo's osd_text does, e.g.
#             curl --data-binary @msg.txt \
#               "http://<camera>/x/json-osd-text.cgi?action=text&slot=1&token=$KEY"
#   POST ?action=save              body: the whole prudynt-osd.json
#        -> replaces the file on the SD card; osd-config applies it within 5 s.
#
# Nothing here writes to flash: settings go to the SD card, text to tmpfs.

. /var/www/x/auth.sh
require_auth

SD=/mnt/mmcblk0p1
CONFIG=$SD/prudynt-osd.json
PRUDYNT_CONFIG=/etc/prudynt.json
TMP=/tmp/osd-text-$$
trap 'rm -f "$TMP" "$TMP.body"' EXIT

send_json() {
	printf 'Status: %s\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n' "${2:-200 OK}"
	printf '%s\n' "$1"
	exit 0
}

fail() {
	send_json "{\"error\":\"$(json_escape "$1")\"}" "${2:-400 Bad Request}"
}

# json_escape <string> - escape for a JSON string (the texts are ASCII, and logread
# lines have nothing beyond quotes, backslashes and tabs worth caring about)
json_escape() {
	printf '%s' "$1" | sed \
		-e 's/\\/\\\\/g' \
		-e 's/"/\\"/g' \
		-e 's/	/\\t/g' \
		-e 's/\r/\\r/g' \
		-e ':a;N;$!ba;s/\n/\\n/g'
}

# Query string: only "action" and "slot", with values checked against a fixed
# set. No eval (run.cgi's way), nothing else is looked at.
action=""
slot=""
set -f # no globbing while splitting the query string
for pair in $(printf '%s' "$QUERY_STRING" | tr '&' ' '); do
	case "$pair" in
	action=status | action=text | action=save) action=${pair#action=} ;;
	slot=1 | slot=2 | slot=3) slot=${pair#slot=} ;;
	esac
done

read_body() {
	case "$CONTENT_LENGTH" in
	"" | *[!0-9]* | 0) : >"$TMP.body" ;;
	*) head -c "$CONTENT_LENGTH" >"$TMP.body" ;;
	esac
}

# slot_path <n> - the file prudynt reads for that slot (osd.textfileN.path), from
# the running prudynt; the documented default when it does not answer
slot_path() {
	local key=textfile p
	[ "$1" != 1 ] && key=textfile$1
	if prudyntctl json "{\"osd\":{\"$key\":null}}" >"$TMP" 2>/dev/null; then
		p=$(jct "$TMP" get "$key.path" 2>/dev/null)
	fi
	case "$p" in
	"") p=/run/prudynt/osd-text${1#1} ;;
	esac
	printf '%s' "$p"
}

case "$action" in
status)
	config=null
	config_error=null
	if [ -f "$CONFIG" ]; then
		# jct prints {} for broken JSON without failing; "get osd" fails, and a
		# file without an "osd" object is useless for osd-config anyway
		if jct "$CONFIG" get osd >/dev/null 2>&1; then
			config=$(jct "$CONFIG" print 2>/dev/null)
		else
			config_error="\"not valid JSON, or no \\\"osd\\\" object\""
		fi
	elif ! mountpoint -q "$SD"; then
		config_error='"SD card not mounted"'
	fi
	pool=$(jct "$PRUDYNT_CONFIG" get general.osd_pool_size 2>/dev/null)
	case "$pool" in "" | *[!0-9]*) pool=0 ;; esac
	live=$(prudyntctl json '{"osd":{"textfile":null,"textfile2":null,"textfile3":null,"burnin":null}}' 2>/dev/null)
	case "$live" in "{"*) ;; *) live=null ;; esac
	texts=""
	for n in 1 2 3; do
		p=$(slot_path $n)
		if [ -f "$p" ]; then
			t="\"$(json_escape "$(cat "$p")")\""
		else
			t=null
		fi
		texts="$texts${texts:+,}\"textfile${n#1}\":$t"
	done
	log=""
	# The patched prudynt tags its warnings "textfile" (reduced to / not shown)
	while IFS= read -r line; do
		[ -n "$line" ] && log="$log${log:+,}\"$(json_escape "$line")\""
	done <<EOF
$(logread 2>/dev/null | grep -E 'textfile|osd-config' | tail -n 12)
EOF
	send_json "{\"config_path\":\"$CONFIG\",\"config\":$config,\"config_error\":$config_error,\"pool_size\":$pool,\"live\":$live,\"texts\":{$texts},\"log\":[$log]}"
	;;

text)
	[ "$REQUEST_METHOD" = POST ] || fail "POST required" "405 Method Not Allowed"
	[ -n "$slot" ] || fail "slot=1|2|3 required"
	p=$(slot_path "$slot")
	# The overlay file belongs on tmpfs (docs/osd.md); refuse anything else
	# rather than write as root wherever osd.textfileN.path points
	case "$p" in
	/run/*/* | /tmp/*) ;;
	*) fail "osd.textfile${slot#1}.path is $p, not on /run or /tmp - not writing there" "409 Conflict" ;;
	esac
	case "$p" in *..*) fail "bad path $p" "409 Conflict" ;; esac
	read_body
	if [ -s "$TMP.body" ]; then
		# Same contract as osd-progress-demo: write next to it, then mv, so
		# prudynt never sees a half-written file
		if ! { cp "$TMP.body" "$p.tmp" && mv "$p.tmp" "$p"; }; then
			rm -f "$p.tmp"
			fail "cannot write $p" "500 Internal Server Error"
		fi
		send_json "{\"ok\":true,\"path\":\"$p\",\"bytes\":$(wc -c <"$p" | tr -d ' ')}"
	else
		rm -f "$p" "$p.tmp"
		send_json "{\"ok\":true,\"path\":\"$p\",\"bytes\":0}"
	fi
	;;

save)
	[ "$REQUEST_METHOD" = POST ] || fail "POST required" "405 Method Not Allowed"
	mountpoint -q "$SD" || fail "SD card not mounted at $SD" "409 Conflict"
	read_body
	[ -s "$TMP.body" ] || fail "empty body"
	# osd-config ignores a file without an "osd" object; do not save one
	jct "$TMP.body" get osd >/dev/null 2>&1 || fail "not valid JSON, or no \"osd\" object"
	# Write on the SD card itself, so the final mv is atomic (the file is
	# what osd-config polls every 5 s)
	if ! { cp "$TMP.body" "$CONFIG.tmp" && mv "$CONFIG.tmp" "$CONFIG"; }; then
		rm -f "$CONFIG.tmp"
		fail "cannot write $CONFIG" "500 Internal Server Error"
	fi
	send_json "{\"ok\":true,\"path\":\"$CONFIG\"}"
	;;

*)
	fail "action=status|text|save required"
	;;
esac
