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
#        -> {"config_path":<SD file Save writes>, "config_source":<file "config" came from:
#            the SD file, else /etc/prudynt-osd.json, like osd-config>, "config":<its content
#            or null>, "config_error":...,
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
BUILTIN_CONFIG=/etc/prudynt-osd.json # osd-config falls back to this one
PRUDYNT_CONFIG=/etc/prudynt.json
# Request bodies are small (a text is at most 128x32 characters, the config a
# few KB); anything bigger is a mistake, not a use case
BODY_MAX=65536
# Scratch files in a private directory with an unpredictable name (root CGI:
# a pre-planted symlink at a guessable /tmp name must not be followed)
umask 077
TMPD=$(mktemp -d /tmp/osd-text.XXXXXX) || { printf 'Status: 500 Internal Server Error\r\n\r\n'; exit 0; }
TMP=$TMPD/answer
trap 'rm -rf "$TMPD"' EXIT
# uhttpd ends a CGI that stalls (a client that sent less than Content-Length
# and hung up) with a signal: EXIT alone does not run then
trap 'rm -rf "$TMPD"; exit 1' HUP INT TERM PIPE

send_json() {
	printf 'Status: %s\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n' "${2:-200 OK}"
	printf '%s\n' "$1"
	exit 0
}

fail() {
	send_json "{\"error\":\"$(json_escape "$1")\"}" "${2:-400 Bad Request}"
}

# json_escape <string> - escape for a JSON string. Control characters other than
# tab / CR / LF are dropped first: JSON.parse rejects them raw, and one stray
# byte in an overlay file or a log line would break the whole status answer
# (prudynt shows them as blanks anyway).
json_escape() {
	printf '%s' "$1" | tr -d '\000-\010\013\014\016-\037\177' | sed \
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

# read_body - the POST body into $TMP.body, exactly CONTENT_LENGTH bytes or an
# error (a short read must not end up as a half text on the overlay)
read_body() {
	case "$CONTENT_LENGTH" in
	"" | *[!0-9]* | 0) : >"$TMP.body" ;;
	*)
		[ "$CONTENT_LENGTH" -le $BODY_MAX ] || fail "body larger than $BODY_MAX bytes" "413 Payload Too Large"
		# uhttpd (-t 0) never ends a CGI whose client sent less than
		# Content-Length and went away: the read would sleep forever. Give it
		# 10 s (the body is a few KB, from the LAN).
		timeout 10 head -c "$CONTENT_LENGTH" >"$TMP.body" || fail "cannot read the body (timeout?)" "400 Bad Request"
		[ "$(wc -c <"$TMP.body" | tr -d ' ')" = "$CONTENT_LENGTH" ] || fail "body shorter than Content-Length" "400 Bad Request"
		;;
	esac
}

# has_osd_object <file> - 0 when the file is JSON with an "osd" object (what
# osd-config applies; it ignores {"osd":[]} or {"osd":null}, so we do too)
has_osd_object() {
	case $(jct "$1" get osd 2>/dev/null) in "{"*) return 0 ;; esac
	return 1
}

# regular_or_absent <path> - 0 unless something that is not a plain file sits
# there (a directory, a symlink): mv would go inside a directory, a symlink
# would be followed as root
regular_or_absent() {
	[ ! -e "$1" ] && [ ! -L "$1" ] && return 0
	[ -f "$1" ] && [ ! -L "$1" ]
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
	config_source=null
	# What the page merges its form into: the file osd-config is using now. Same
	# search order as osd-config (SD card, then the built-in file), so that a
	# first Save to the SD card carries over keys the page does not show
	# (osd.textfile.path, substream_disabled, ...) instead of dropping them.
	if [ -f "$CONFIG" ]; then
		src=$CONFIG
	elif [ -f "$BUILTIN_CONFIG" ]; then
		src=$BUILTIN_CONFIG
	else
		src=""
	fi
	if [ -n "$src" ]; then
		# jct prints {} for broken JSON without failing; "get osd" fails, and a
		# file without an "osd" object is useless for osd-config anyway
		if has_osd_object "$src"; then
			config=$(jct "$src" print 2>/dev/null)
			config_source="\"$src\""
		else
			config_error="\"$src: not valid JSON, or no \\\"osd\\\" object\""
		fi
	fi
	mountpoint -q "$SD" || config_error='"SD card not mounted"'
	pool=$(jct "$PRUDYNT_CONFIG" get general.osd_pool_size 2>/dev/null)
	case "$pool" in "" | *[!0-9]*) pool=0 ;; esac
	live=$(prudyntctl json '{"osd":{"textfile":null,"textfile2":null,"textfile3":null,"imagefile":null,"burnin":null}}' 2>/dev/null)
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
	# The image slot's file is binary: report its size only (null = none)
	ip=""
	if [ "$live" != null ]; then
		printf '%s' "$live" >"$TMP"
		ip=$(jct "$TMP" get imagefile.path 2>/dev/null)
	fi
	[ -n "$ip" ] || ip=/run/prudynt/osd-image
	if [ -f "$ip" ]; then
		texts="$texts,\"imagefile\":$(wc -c <"$ip" | tr -d ' ')"
	else
		texts="$texts,\"imagefile\":null"
	fi
	log=""
	# The patched prudynt tags its warnings "textfile" / "imagefile" (reduced to / not shown)
	while IFS= read -r line; do
		[ -n "$line" ] && log="$log${log:+,}\"$(json_escape "$line")\""
	done <<EOF
$(logread 2>/dev/null | grep -E 'textfile|imagefile|osd-config|osd-feed' | tail -n 12)
EOF
	send_json "{\"config_path\":\"$CONFIG\",\"config_source\":$config_source,\"config\":$config,\"config_error\":$config_error,\"pool_size\":$pool,\"live\":$live,\"texts\":{$texts},\"log\":[$log]}"
	;;

text)
	[ "$REQUEST_METHOD" = POST ] || fail "POST required" "405 Method Not Allowed"
	[ -n "$slot" ] || fail "slot=1|2|3 required"
	p=$(slot_path "$slot")
	# The overlay file belongs on tmpfs (docs/osd.md); refuse anything else
	# rather than write as root wherever osd.textfileN.path points
	case "$p" in
	/run/* | /tmp/*) ;;
	*) fail "osd.textfile${slot#1}.path is $p, not on /run or /tmp - not writing there" "409 Conflict" ;;
	esac
	case "$p" in *..*) fail "bad path $p" "409 Conflict" ;; esac
	pj=$(json_escape "$p")
	regular_or_absent "$p" || fail "$p exists and is not a regular file - not touching it" "409 Conflict"
	read_body
	if [ -s "$TMP.body" ]; then
		# Same contract as osd-progress-demo: write next to it, then mv, so
		# prudynt never sees a half-written file. The temp name carries the pid:
		# two requests for the same slot at once must not share one
		if ! { cp "$TMP.body" "$p.tmp.$$" && chmod 644 "$p.tmp.$$" && mv "$p.tmp.$$" "$p"; }; then
			rm -f "$p.tmp.$$"
			fail "cannot write $p" "500 Internal Server Error"
		fi
		send_json "{\"ok\":true,\"path\":\"$pj\",\"bytes\":$CONTENT_LENGTH}"
	else
		rm -f "$p" "$p.tmp.$$"
		[ -e "$p" ] && fail "cannot remove $p" "500 Internal Server Error"
		send_json "{\"ok\":true,\"path\":\"$pj\",\"bytes\":0}"
	fi
	;;

save)
	[ "$REQUEST_METHOD" = POST ] || fail "POST required" "405 Method Not Allowed"
	mountpoint -q "$SD" || fail "SD card not mounted at $SD" "409 Conflict"
	regular_or_absent "$CONFIG" || fail "$CONFIG exists and is not a regular file - not touching it" "409 Conflict"
	read_body
	[ -s "$TMP.body" ] || fail "empty body"
	# osd-config ignores a file without an "osd" object; do not save one
	has_osd_object "$TMP.body" || fail "not valid JSON, or no \"osd\" object"
	# Write on the SD card itself, so the final mv is atomic (the file is
	# what osd-config polls every 5 s)
	if ! { cp "$TMP.body" "$CONFIG.tmp.$$" && chmod 644 "$CONFIG.tmp.$$" && mv "$CONFIG.tmp.$$" "$CONFIG"; }; then
		rm -f "$CONFIG.tmp.$$"
		fail "cannot write $CONFIG" "500 Internal Server Error"
	fi
	send_json "{\"ok\":true,\"path\":\"$CONFIG\"}"
	;;

*)
	fail "action=status|text|save required"
	;;
esac
