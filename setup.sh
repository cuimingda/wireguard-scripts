#!/bin/bash

set -euo pipefail

fail() {
	printf 'setup.sh: %s\n' "$1" >&2
	exit 1
}

has_apt_candidate() {
	LC_ALL=C apt-cache policy "$1" | awk '$1 == "Candidate:" && $2 != "(none)" { found = 1 } END { exit !found }'
}

main() {
	local binary=${DESTDIR:-}/usr/local/bin/wgk go_package=golang-1.26-go
	local release=/etc/os-release ID='' VERSION_CODENAME='' mirror
	local -a elevate=()

	cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
	if [[ -f $binary && -x $binary ]]; then
		printf '%s already exists. Skipping setup.\n' "$binary"
		return
	fi

	[[ $(uname -s) == Linux ]] || fail 'setup requires Ubuntu Linux'
	[[ -e $release ]] || release=/usr/lib/os-release
	[[ -r $release ]] || fail "cannot read $release to identify Ubuntu"
	# shellcheck source=/dev/null
	source "$release"
	[[ $ID == ubuntu ]] || fail 'setup requires Ubuntu Linux'
	case $(uname -m) in
	x86_64) mirror=http://archive.ubuntu.com/ubuntu ;;
	aarch64) mirror=http://ports.ubuntu.com/ubuntu-ports ;;
	*) fail 'setup requires Ubuntu on amd64 or arm64' ;;
	esac

	if ((EUID != 0)); then
		command -v sudo >/dev/null || fail 'installing APT packages requires root or sudo'
		elevate=(sudo --)
	fi

	"${elevate[@]}" apt-get update
	if ! has_apt_candidate "$go_package"; then
		local source_file=/etc/apt/sources.list.d/wgk-ubuntu-universe.sources
		local no_candidate="APT cannot find $go_package. Check that the official Ubuntu updates and universe sources are available for this release."
		case $VERSION_CODENAME in
		jammy | noble) ;;
		*) fail "$no_candidate" ;;
		esac
		[[ ! -e $source_file && ! -L $source_file ]] || fail "$no_candidate"

		printf 'Enabling the official %s-updates universe source for %s.\n' "$VERSION_CODENAME" "$go_package"
		printf 'Types: deb\nURIs: %s\nSuites: %s-updates\nComponents: universe\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' \
			"$mirror" "$VERSION_CODENAME" | "${elevate[@]}" tee "$source_file" >/dev/null
		if ! "${elevate[@]}" apt-get update || ! has_apt_candidate "$go_package"; then
			"${elevate[@]}" rm -f -- "$source_file"
			fail "$no_candidate"
		fi
	fi
	"${elevate[@]}" apt-get install -y --no-install-recommends make "$go_package" ca-certificates
	make install
}

main
