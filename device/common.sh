# common.sh - shared by install.sh and disable-netwatch.sh (sourced, not run).
#
# Everything in this directory is written against ONE Thingino build: the ffmpeg
# binary depends on its toolchain/mbedTLS, the scripts on its init layout and on
# netwatch being present. Refuse anything else instead of half-working on it.
# Bump this together with the build docs when moving to a newer firmware.
SUPPORTED_BUILD="ciao+da40db6"

# check_camera root@host - abort unless the camera is reachable and runs SUPPORTED_BUILD
check_camera() {
	if ! build=$(ssh "$1" '. /etc/os-release && echo "${BUILD_ID%%,*}"'); then
		echo "Cannot reach $1 over ssh - nothing was changed" >&2
		exit 1
	fi
	if [ "$build" != "$SUPPORTED_BUILD" ]; then
		echo "Unsupported firmware: camera runs '$build', these files target '$SUPPORTED_BUILD'." >&2
		echo "See the firmware table in the top-level README. Nothing was changed." >&2
		exit 1
	fi
	echo "Camera: $1 ($build)"
}
