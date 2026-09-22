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
#   POST ?action=service&op=restart-prudynt
#        -> stop prudynt, wait until it is really gone, start it, wait until it
#           answers. Thingino's own "service restart prudynt" starts the new one
#           while the old one is still shutting down, and the new one then quits
#           with "Another Prudynt instance appears to be running" - leaving no
#           prudynt at all (seen on this camera).
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
	op=start | op=stop | op=restart | op=enable | op=disable | op=restart-prudynt) op=${pair#op=} ;;
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

# restart_prudynt - the sequence osd-config uses (device/osd-config): stop, wait
# for the process to be gone (its init script returns early), start, wait for
# an answer. Sends the JSON reply itself.
PRUDYNT_INIT=/etc/init.d/S31prudynt
PRUDYNT_LOCK=/run/prudynt-restart.lock # shared with osd-config (its pool-size restart)
restart_prudynt() {
	[ -f "$PRUDYNT_INIT" ] || fail "$PRUDYNT_INIT not found" "409 Conflict"
	if ! mkdir "$PRUDYNT_LOCK" 2>/dev/null; then
		now=$(date +%s)
		since=$(cat "$PRUDYNT_LOCK/since" 2>/dev/null)
		case "$since" in "" | *[!0-9]*) since=$(stat -c %Y "$PRUDYNT_LOCK" 2>/dev/null || echo 0) ;; esac
		[ $((now - since)) -lt 120 ] && fail "prudynt is being restarted by something else (osd-config?) - try again in a moment" "409 Conflict"
		mv "$PRUDYNT_LOCK" "$PRUDYNT_LOCK.stale.$$" 2>/dev/null && rm -rf "$PRUDYNT_LOCK.stale.$$"
		mkdir "$PRUDYNT_LOCK" 2>/dev/null || fail "prudynt is being restarted by something else" "409 Conflict"
	fi
	echo "$$" >"$PRUDYNT_LOCK/owner"
	date +%s >"$PRUDYNT_LOCK/since"
	trap 'unlock; prudynt_unlock; rm -rf "$TMPD"' EXIT
	trap 'unlock; prudynt_unlock; rm -rf "$TMPD"; exit 1' HUP INT TERM PIPE
	t0=$(date +%s)
	sh "$PRUDYNT_INIT" stop >/dev/null 2>&1
	i=0
	while pidof prudynt >/dev/null && [ $i -lt 20 ]; do
		sleep 1
		i=$((i + 1))
	done
	pidof prudynt >/dev/null && fail "prudynt did not stop within 20 s - not starting a second one" "500 Internal Server Error"
	sh "$PRUDYNT_INIT" start >/dev/null 2>&1
	i=0
	while [ $i -lt 30 ]; do
		sleep 1
		i=$((i + 1))
		case $(prudyntctl json '{"general":{"loglevel":null}}' 2>/dev/null) in
		"{"*) send_json "{\"ok\":true,\"op\":\"restart-prudynt\",\"pid\":$(pidof prudynt | cut -d' ' -f1),\"seconds\":$(($(date +%s) - t0))}" ;;
		esac
		[ $i -ge 5 ] && ! pidof prudynt >/dev/null && fail "prudynt exited right after starting - see logread" "500 Internal Server Error"
	done
	fail "prudynt started but does not answer after 30 s" "500 Internal Server Error"
}

# One save / service operation at a time: a Stop arriving while a restart
# still waits for the old supervisor would remove the pidfile the restart is
# about to create, or start a second supervisor; two saves would race on the
# file. mkdir is atomic. A lock older than 2 minutes is stale (a CGI killed
# mid-way): it is moved aside with mv (atomic too, so only one of several
# racing requests gets to remove it) and then taken with a fresh mkdir. The
# lock records its owner, and only the owner removes it on exit.
LOCK=/run/youtube-cgi.lock
unlock() {
	[ "$(cat "$LOCK/owner" 2>/dev/null)" = "$$" ] && rm -rf "$LOCK"
}
prudynt_unlock() {
	[ "$(cat "$PRUDYNT_LOCK/owner" 2>/dev/null)" = "$$" ] && rm -rf "$PRUDYNT_LOCK"
}
service_lock() {
	if ! mkdir "$LOCK" 2>/dev/null; then
		now=$(date +%s)
		# A CGI that died between mkdir and writing "since" leaves a lock
		# without it: judge that one by the directory's own mtime
		since=$(cat "$LOCK/since" 2>/dev/null)
		case "$since" in "" | *[!0-9]*) since=$(stat -c %Y "$LOCK" 2>/dev/null || echo 0) ;; esac
		if [ $((now - since)) -lt 120 ]; then
			fail "another save or service operation is still running - try again in a moment" "409 Conflict"
		fi
		if mv "$LOCK" "$LOCK.stale.$$" 2>/dev/null; then
			rm -rf "$LOCK.stale.$$"
		fi
		mkdir "$LOCK" 2>/dev/null || fail "another save or service operation is still running" "409 Conflict"
	fi
	echo "$$" >"$LOCK/owner"
	date +%s >"$LOCK/since"
	trap 'unlock; rm -rf "$TMPD"' EXIT
	trap 'unlock; rm -rf "$TMPD"; exit 1' HUP INT TERM PIPE
}

case "$action" in
status)
	config=null
	config_error=null
	config_source=null
	if src=$(config_source); then
		# jct prints {} for broken JSON without failing: check a key it must have
		# jct prints {} for broken JSON without failing: the relay needs at
		# least a stream_key, so that key tells a config from a broken file
		if jct "$src" get stream_key >/dev/null 2>&1; then
			config=$(jct "$src" print 2>/dev/null)
			config_source="\"$src\""
		else
			config_error="\"$src: not valid JSON, or no \\\"stream_key\\\"\""
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
	# The ffmpeg started by the relay: a child of the supervisor whose command
	# line carries the relay's fixed first options (youtube-relay: set --
	# -nostdin -hide_banner ...). Not by name: ffmpeg_bin may point anywhere,
	# and the web UI's recorder runs its own ffmpeg.
	ffmpeg='{"running":false,"pid":null,"elapsed_s":null}'
	for d in /proc/[0-9]*; do
		ppid=$(cut -d' ' -f4 "$d/stat" 2>/dev/null) || continue
		is_relay "$ppid" || continue
		tr '\0' ' ' <"$d/cmdline" 2>/dev/null | grep -q -- ' -nostdin -hide_banner ' || continue
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
	# The relay's own rules (youtube-relay: config_ok): a JSON object; unless
	# enabled is false it needs a stream_key, or it would stop the stream within
	# 15 s. rtmp_url may be left out (the relay defaults to YouTube's RTMPS)
	# but must be an rtmp(s) URL when given. jct fails on broken JSON.
	case $(jct "$TMPD/body" print 2>/dev/null | head -c 1) in
	"{") ;;
	*) fail "not a JSON object" ;;
	esac
	jct "$TMPD/body" get stream_key >/dev/null 2>&1 || fail "no \"stream_key\" (the relay ignores the file without it)"
	if [ "$(jct "$TMPD/body" get enabled 2>/dev/null)" != "false" ] && [ -z "$(jct "$TMPD/body" get stream_key 2>/dev/null)" ]; then
		fail "stream_key is empty: the relay would stop the stream. Set a key, or set enabled to false"
	fi
	case $(jct "$TMPD/body" get rtmp_url 2>/dev/null) in
	"" | rtmp://* | rtmps://*) ;;
	*) fail "rtmp_url must start with rtmp:// or rtmps://" ;;
	esac
	# From here on nothing else may write the file or restart the relay: a
	# second save must not slip in between our write and our restart
	service_lock
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
	[ -n "$op" ] || fail "op=start|stop|restart|enable|disable|restart-prudynt required"
	if [ "$op" = restart-prudynt ]; then
		service_lock
		restart_prudynt # always answers and exits
		exit 0
	fi
	[ -f "$INIT" ] || fail "$INIT is not installed" "409 Conflict"
	service_lock
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
