# common.sh - shared by the install-*.sh / disable-netwatch.sh scripts (sourced, not run).
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

# push <local> <remote> <mode> - copy a file into place atomically (needs $CAM)
# (write to .new, chmod, mv; clean up on failure).
# Thingino has no sftp-server, so plain scp fails. "scp -O" (legacy protocol)
# works and is the simpler choice by hand, but here it would not save anything:
# the chmod/mv/cleanup needs an ssh call anyway, -O is rejected by older OpenSSH
# clients, and it needs an scp binary on the camera. ssh + cat has none of that.
push() {
	echo "  $1 -> $2"
	if ! ssh "$CAM" "cat > '$2.new' && chmod $3 '$2.new' && mv '$2.new' '$2' || { rm -f '$2.new'; exit 1; }" <"$1"; then
		echo "Failed to write $2 (overlay full? check: ssh $CAM df -h /overlay)" >&2
		exit 1
	fi
}
