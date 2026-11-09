#!/bin/sh
# install-agent-adapter.sh - make the Web UI "Rec ch0" button work (docs/recording.md).
# Run on the PC:
#   device/install-agent-adapter.sh root@<camera-ip>
#
# The Web UI's recorder button talks to Thingino's agent API, which hands the
# request to a backend adapter at /usr/libexec/agent/adapter.sh. The ciao+da40db6
# firmware ships the "null" adapter there (the prudynt one from the same source
# tree never made it into the image), so every press ends in "Failed to start
# recording on channel 0". This puts the prudynt adapter in place, with one fix
# (patches/thingino-agent-adapter-record-args.diff: `record start -c N` must be
# `record -c N start` for the record script on this build). The old file is
# kept as adapter.sh.null. Takes effect immediately (the agent re-reads the
# adapter on every request). Safe to re-run. Undo:
#   ssh root@<camera-ip> 'mv /usr/libexec/agent/adapter.sh.null /usr/libexec/agent/adapter.sh'

set -e

CAM=$1
HERE=$(cd "$(dirname "$0")" && pwd)
ADAPTER=/usr/libexec/agent/adapter.sh
NULL_MD5=412f388db68f9af7b5c554429100e7d4 # thingino-agent/files/agent-adapter-null at ciao+da40db6

if [ -z "$CAM" ]; then
	echo "Usage: $0 root@<camera-ip>" >&2
	exit 1
fi

. "$HERE/common.sh"
check_camera "$CAM"

ours=$(md5sum "$HERE/agent-adapter" | cut -d' ' -f1)
current=$(ssh "$CAM" "md5sum $ADAPTER 2>/dev/null | cut -d' ' -f1")
case "$current" in
"$ours")
	echo "Already installed ($ADAPTER matches device/agent-adapter)"
	;;
"$NULL_MD5")
	echo "Installing the prudynt agent adapter (current: null adapter, kept as $ADAPTER.null)"
	ssh "$CAM" "[ -f $ADAPTER.null ] || cp $ADAPTER $ADAPTER.null"
	push "$HERE/agent-adapter" "$ADAPTER" 644
	;;
*)
	echo "Unexpected $ADAPTER on the camera (md5 '$current'): not the null adapter from" >&2
	echo "this firmware and not ours. Look at it first; nothing was changed." >&2
	exit 1
	;;
esac

if ! ssh "$CAM" 'agentctl capabilities streams 2>/dev/null | grep -q "\"clip_recording\":true"'; then
	echo "agentctl does not report clip_recording after the install - check: ssh $CAM agentctl capabilities streams" >&2
	exit 1
fi
echo "Done. The Web UI Rec ch0 button now starts/stops the SD recording (reload the page)."
