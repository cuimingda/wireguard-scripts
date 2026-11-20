#!/bin/bash

set -euo pipefail

fail() {
	printf 'make install: %s\n' "$1" >&2
	exit 1
}

install_binary() (
	set -euo pipefail
	umask 022
	local binary=$1 destination=$2
	mkdir -p -- "$destination"
	staged=$(mktemp -- "$destination/.wgk.XXXXXXXXXX")
	trap 'rm -f -- "$staged"' EXIT
	install -m 0755 -- "$binary" "$staged"
	mv -fT -- "$staged" "$destination/wgk"
)

main() (
	local arch destination=${DESTDIR:-}/usr/local/bin
	[[ $(uname -s) == Linux ]] || fail 'make install targets Ubuntu Linux'
	case $(uname -m) in
	x86_64) arch=amd64 ;;
	aarch64) arch=arm64 ;;
	*) fail 'make install needs a linux/amd64 or linux/arm64 host' ;;
	esac

	command -v -- "$GO" >/dev/null || fail "Go executable '$GO' is unavailable. Run ./setup.sh for a first install or set GO to an installed Go executable."

	build_dir=$(mktemp -d /tmp/wgk-install.XXXXXXXXXX)
	trap 'rm -rf -- "$build_dir"' EXIT
	GOROOT='' GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
		"$GO" build -mod=readonly -o "$build_dir/wgk" ./cmd/wgk

	if [[ -n ${DESTDIR:-} || -w $destination || $EUID -eq 0 ]]; then
		install_binary "$build_dir/wgk" "$destination"
	else
		command -v sudo >/dev/null || fail "write access to $destination or sudo is required"
		sudo -- /bin/bash -c "$(declare -f install_binary)
install_binary \"\$@\"" install-wgk "$build_dir/wgk" "$destination"
	fi
	printf 'Installed %s/wgk\n' "$destination"
)

main
