#!/bin/sh
# concat-recordings.sh - join prudynt recorder clips (1 file per minute) into one MP4
#
#   tools/concat-recordings.sh <clip dir> <output.mp4>
#
# Needs ffmpeg/ffprobe on the Mac (brew install ffmpeg). No re-encoding.
#
# Why not a plain `ffmpeg -f concat -c copy`: prudynt's SimpleMP4Muxer does not
# keep the video and audio tracks on the same time base from one file to the
# next (video may start at 179.8s while audio starts at 0s). A plain concat
# then stretches the result to several times its real length. So each clip is
# split into a video-only and an audio-only file, both re-based to start at 0,
# the two tracks are concatenated separately with the audio clip lengths as
# the offsets for both (the video clock runs ~10 s/h slow against the audio
# clock, so audio is the reference), and finally muxed back together.
set -eu

DIR=${1:?usage: $0 <clip dir> <output.mp4>}
OUT=${2:?usage: $0 <clip dir> <output.mp4>}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/v" "$WORK/a"
: >"$WORK/v.txt"
: >"$WORK/a.txt"

# Clips are numbered in sorted path order: the recorder's file names repeat
# across days (%Y/%m/%d/%H-%M-%S), so the basename alone is not unique. The
# concat lists use paths relative to the list file, so $WORK needs no quoting.
i=0
find "$DIR" -name '*.mp4' -print | sort | while IFS= read -r f; do
	i=$((i + 1))
	b=$(printf '%06d.mp4' "$i")
	ffmpeg -v error -y -i "$f" -map 0:v -c copy -avoid_negative_ts make_zero "$WORK/v/$b"
	ffmpeg -v error -y -i "$f" -map 0:a -c copy -avoid_negative_ts make_zero "$WORK/a/$b"
	# audio span of this clip (last packet's pts + its duration) = offset for
	# the next clip in both tracks. Falls back to one AAC frame (1024 samples)
	# when ffprobe gives no packet duration.
	sr=$(ffprobe -v error -select_streams a:0 -show_entries stream=sample_rate -of csv=p=0 "$WORK/a/$b")
	dur=$(ffprobe -v error -select_streams a:0 -show_entries packet=pts_time,duration_time -of csv=p=0 "$WORK/a/$b" |
		awk -F, -v sr="${sr:-16000}" '{last=$1; d=$2} END{if (d !~ /^[0-9.]+$/) d = 1024 / sr; printf "%.3f", last + d}')
	printf "file 'v/%s'\nduration %s\n" "$b" "$dur" >>"$WORK/v.txt"
	printf "file 'a/%s'\nduration %s\n" "$b" "$dur" >>"$WORK/a.txt"
done
n=$(grep -c '^file ' "$WORK/v.txt") || true
[ "$n" -gt 0 ] || { echo "no .mp4 files under $DIR" >&2; exit 1; }

ffmpeg -v error -y -f concat -safe 0 -i "$WORK/v.txt" -c copy "$WORK/v-all.mp4"
ffmpeg -v error -y -f concat -safe 0 -i "$WORK/a.txt" -c copy "$WORK/a-all.mp4"
ffmpeg -v error -y -i "$WORK/v-all.mp4" -i "$WORK/a-all.mp4" -map 0:v -map 1:a -c copy "$OUT"

echo "$n clips -> $OUT"
ffprobe -v error -show_entries stream=codec_type,duration -of csv=p=0 "$OUT"
