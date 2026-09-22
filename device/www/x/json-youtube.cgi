#!/bin/sh
# shellcheck disable=SC1091,SC3043
# json-youtube.cgi - backend of /youtube.html (Thingino web UI page for the
# YouTube Live relay, docs/relay.md). Install to /var/www/x/json-youtube.cgi.
# Runs under uhttpd as root like Thingino's own CGIs; authentication is
# Thingino's (web UI session cookie, or ?token=<API key>).
#
#   GET  ?action=status
#        -> {"config_path": <file Save writes>, "config_source": <file the relay uses now
#            or null>, "config": <its content or null>, "config_error": ...,
#            "sd_mounted": bool,
#            "service": {"running": bool, "pid": n, "autostart": bool},
#            "ffmpeg":  {"running": bool, "pid": n, "elapsed_s": n},
#            "log": [...]}   (last logread lines of youtube-relay; keys are redacted by the relay)
#   POST ?action=save[&restart=1]   body: the whole youtube-relay.json
#        -> replaces the config file (tmp + mv, 600); with restart=1 the relay is
#           restarted afterwards so a new key / URL takes effect (the stream
#           drops for a few seconds). Without it the relay notices "enabled"
#           by itself within 15 s, but keeps streaming with the old key/URL.
#   POST ?action=service&op=start|stop|restart|enable|disable
#        -> "service <op> youtube-relay" (enable/disable = start at boot)
#
# The stream key goes to the authenticated browser in clear, as Thingino does
# with the API key and the Wi-Fi password.

. /var/www/x/auth.sh
require_auth

SD=/mnt/mmcblk0p1
SD_CONFIG=$SD/youtube-relay.json
ETC_CONFIG=/etc/youtube-relay.json
INIT=/etc/init.d/S93youtube-relay
PIDFILE=/run/youtube-relay.pid
BODY_MAX=65536

umask 077
TMPD=$(mktemp -d /tmp/youtube-cgi.XXXXXX) || { printf 'Status: 500 Internal Server Error\r\n\r\n'; exit 0; }
trap 'rm -rf "$TMPD"' EXIT
trap 'rm -rf "$TMPD"; exit 1' HUP INT TERM PIPE

send_json() {
	printf 'Status: %s\r\nContent-Type: application/json\r\nCache-Control: no-store\r\nConnection: close\r\n\r\n' "${2:-200 OK}"
	printf '%s\n' "$1"
	exit 0
}

fail() {
	send_json "{\"error\":\"$(json_escape "$1")\"}" "${2:-400 Bad Request}"
}

json_escape() {
	printf '%s' "$1" | tr -d '\000-\010\013\014\016-\037\177' | sed \
		-e 's/\\/\\\\/g' \
		-e 's/"/\\"/g' \
		-e 's/	/\\t/g' \
		-e 's/\r/\\r/g' \
		-e ':a;N;$!ba;s/\n/\\n/g'
}

action=""
op=""
restart=""
set -f
for pair in $(printf '%s' "$QUERY_STRING" | tr '&' ' '); do
	case "$pair" in
	action=status | action=save | action=service) action=${pair#action=} ;;
	op=start | op=stop | op=restart | op=enable | op=disable) op=${pair#op=} ;;
	restart=1) restart=1 ;;
	esac
done
set +f

read_body() {
	case "$CONTENT_LENGTH" in
	"" | *[!0-9]* | 0) : >"$TMPD/body" ;;
	*)
		[ "$CONTENT_LENGTH" -le $BODY_MAX ] || fail "body larger than $BODY_MAX bytes" "413 Payload Too Large"
		timeout 10 head -c "$CONTENT_LENGTH" >"$TMPD/body" || fail "cannot read the body (timeout?)" "400 Bad Request"
		[ "$(wc -c <"$TMPD/body" | tr -d ' ')" = "$CONTENT_LENGTH" ] || fail "body shorter than Content-Length" "400 Bad Request"
		;;
	esac
}

# The relay's own rules (youtube-relay: find_config): SD card first, then /etc
config_source() {
	for c in $SD_CONFIG $ETC_CONFIG; do
		[ -f "$c" ] && { printf '%s' "$c"; return 0; }
	done
	return 1
}

# Where Save writes: the file in use, else the SD card if there is one, else /etc
config_target() {
	config_source && return 0
	if mountpoint -q "$SD"; then printf '%s' "$SD_CONFIG"; else printf '%s' "$ETC_CONFIG"; fi
}

# is_relay <pid> - that pid is the supervisor (pidfiles outlive processes)
is_relay() {
	[ -n "$1" ] && grep -q youtube-relay "/proc/$1/cmdline" 2>/dev/null
}

regular_or_absent() {
	[ ! -e "$1" ] && [ ! -L "$1" ] && return 0
	[ -f "$1" ] && [ ! -L "$1" ]
}

case "$action" in
status)
	config=null
	config_error=null
	config_source=null
	if src=$(config_source); then
		# jct prints {} for broken JSON without failing: check a key it must have
		if jct "$src" get rtmp_url >/dev/null 2>&1 || jct "$src" get stream_key >/dev/null 2>&1; then
			config=$(jct "$src" print 2>/dev/null)
			config_source="\"$src\""
		else
			config_error="\"$src: not valid JSON\""
		fi
	fi
	sd=false
	mountpoint -q "$SD" && sd=true
	pid=$(cat "$PIDFILE" 2>/dev/null)
	if is_relay "$pid"; then
		service="{\"running\":true,\"pid\":$pid"
	else
		service='{"running":false,"pid":null'
	fi
	if [ -x "$INIT" ]; then service="$service,\"autostart\":true}"; else service="$service,\"autostart\":false}"; fi
	# ffmpeg started by the relay (not any ffmpeg: the web UI's recorder has its own)
	ffmpeg='{"running":false,"pid":null,"elapsed_s":null}'
	for d in /proc/[0-9]*; do
		[ "$(cat "$d/stat" 2>/dev/null | cut -d' ' -f2)" = "(ffmpeg)" ] || continue
		ppid=$(cut -d' ' -f4 "$d/stat" 2>/dev/null)
		is_relay "$ppid" || continue
		fp=${d#/proc/}
		# elapsed = uptime - start time (field 22 of stat, in USER_HZ = 100 ticks)
		start=$(cut -d' ' -f22 "$d/stat" 2>/dev/null)
		up=$(cut -d. -f1 /proc/uptime)
		el=$((up - start / 100))
		ffmpeg="{\"running\":true,\"pid\":$fp,\"elapsed_s\":$el}"
		break
	done
	log=""
	while IFS= read -r line; do
		[ -n "$line" ] && log="$log${log:+,}\"$(json_escape "$line")\""
	done <<EOF
$(logread 2>/dev/null | grep -E 'youtube-relay' | tail -n 20)
EOF
	send_json "{\"config_path\":\"$(config_target)\",\"config_source\":$config_source,\"config\":$config,\"config_error\":$config_error,\"sd_mounted\":$sd,\"service\":$service,\"ffmpeg\":$ffmpeg,\"log\":[$log]}"
	;;

save)
	[ "$REQUEST_METHOD" = POST ] || fail "POST required" "405 Method Not Allowed"
	target=$(config_target)
	regular_or_absent "$target" || fail "$target exists and is not a regular file - not touching it" "409 Conflict"
	read_body
	[ -s "$TMPD/body" ] || fail "empty body"
	# What the relay needs to read it: a JSON object with rtmp_url (jct fails on broken JSON)
	jct "$TMPD/body" get rtmp_url >/dev/null 2>&1 || fail "not valid JSON, or no \"rtmp_url\""
	case $(jct "$TMPD/body" get rtmp_url 2>/dev/null) in
	rtmp://* | rtmps://*) ;;
	*) fail "rtmp_url must start with rtmp:// or rtmps://" ;;
	esac
	# Same directory, then mv: the relay (and its watcher, every 15 s) never
	# reads a half-written file. 600 like install.sh: the key is in there.
	if ! { cp "$TMPD/body" "$target.tmp.$$" && chmod 600 "$target.tmp.$$" && mv "$target.tmp.$$" "$target"; }; then
		rm -f "$target.tmp.$$"
		fail "cannot write $target" "500 Internal Server Error"
	fi
	restarted=false
	if [ -n "$restart" ]; then
		# The init script's stop waits for the supervisor (and its ffmpeg) to be
		# gone, so the new one never publishes alongside the old one
		if out=$(sh "$INIT" restart 2>&1); then
			restarted=true
		else
			send_json "{\"ok\":true,\"path\":\"$(json_escape "$target")\",\"restarted\":false,\"error\":\"saved, but the restart failed: $(json_escape "$out")\"}"
		fi
	fi
	send_json "{\"ok\":true,\"path\":\"$(json_escape "$target")\",\"restarted\":$restarted}"
	;;

service)
	[ "$REQUEST_METHOD" = POST ] || fail "POST required" "405 Method Not Allowed"
	[ -n "$op" ] || fail "op=start|stop|restart|enable|disable required"
	[ -f "$INIT" ] || fail "$INIT is not installed" "409 Conflict"
	# start/stop/restart mean "now", independent of "at boot": run the script
	# through sh, so it works while disabled (no execute bit) too.
	# enable/disable = the execute bit, via "service" like the docs say.
	case "$op" in
	enable | disable) out=$(service "$op" youtube-relay 2>&1) ;;
	*) out=$(sh "$INIT" "$op" 2>&1) ;;
	esac
	rc=$?
	if [ $rc -ne 0 ]; then
		fail "service $op failed: $out" "500 Internal Server Error"
	fi
	send_json "{\"ok\":true,\"op\":\"$op\",\"output\":\"$(json_escape "$out")\"}"
	;;

*)
	fail "action=status|save|service required"
	;;
esac
