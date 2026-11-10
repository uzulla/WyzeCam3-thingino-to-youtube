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

# check_pubkeys <file> - abort unless the file is an authorized_keys file: every
# line a public key (ssh-keygen must parse it; a private key, options such as
# command="...", or a truncated key fail), a comment or blank. Used before a
# key file goes to the card (make-sdcard.sh -a, install-keys-from-sd.sh): the
# camera's S37keys-from-sd only looks at the shape of the lines, and a key it
# accepts but ssh cannot use would lock everyone out.
check_pubkeys() {
	keyre='^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com) [A-Za-z0-9+/]+=*( |$)'
	n=0
	tmpkey=$(mktemp) || exit 1
	while IFS= read -r line || [ -n "$line" ]; do
		line=$(printf '%s' "$line" | tr -d '\r')
		[ -n "$(printf '%s' "$line" | tr -d '[:space:]')" ] || continue # blank
		case "$line" in
		\#*) continue ;; # comment
		esac
		if ! printf '%s\n' "$line" | grep -q -E "$keyre"; then
			rm -f "$tmpkey"
			echo "$1: not an SSH public key line: '$(printf '%s' "$line" | cut -c1-40)...' (expected 'ssh-ed25519 AAAA... comment'; a private key file?)" >&2
			exit 1
		fi
		if command -v ssh-keygen >/dev/null 2>&1; then
			printf '%s\n' "$line" >"$tmpkey"
			if ! ssh-keygen -l -f "$tmpkey" >/dev/null 2>&1; then
				rm -f "$tmpkey"
				echo "$1: ssh-keygen cannot read this key: '$(printf '%s' "$line" | cut -c1-40)...'" >&2
				exit 1
			fi
		fi
		n=$((n + 1))
	done <"$1"
	rm -f "$tmpkey"
	if [ "$n" = 0 ]; then
		echo "$1: no SSH public key in it (only comments or blank lines)" >&2
		exit 1
	fi
	echo "$n"
}
