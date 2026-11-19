#!/bin/bash

set -euo pipefail

fail() {
	printf 'make install: %s\n' "$1" >&2
	exit 1
}

has_supported_go() {
	local version
	version=$(GOTOOLCHAIN=local GOWORK=off go env GOVERSION 2>/dev/null) || return 1
	[[ $version =~ ^go([1-9][0-9]*)\.(0|[1-9][0-9]*)(\.[0-9]+)?$ ]] || return 1
	((BASH_REMATCH[1] > 1 || BASH_REMATCH[2] >= 26))
}

prepare_toolchain() (
	set -euo pipefail
	umask 077
	local arch=$1 version=1.27.4 checksum cache_home toolchains cached archive url
	case "$arch" in
	amd64) checksum=7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840 ;;
	arm64) checksum=3392582988a273a87ab5043ee1b2c9c17a2c3c00580bf9326c47a2e0aaaeac92 ;;
	esac

	cache_home=${XDG_CACHE_HOME:-}
	if [[ $cache_home != /* ]]; then
		[[ ${HOME:-} == /* ]] || fail 'set HOME or XDG_CACHE_HOME to an absolute path to cache Go'
		cache_home=$HOME/.cache
	fi
	toolchains=$cache_home/wireguard-toolkit/toolchains
	cached=$toolchains/go${version}-linux-${arch}
	if [[ -x $cached/bin/go ]]; then
		printf '%s\n' "$cached/bin/go"
		return
	fi

	command -v curl >/dev/null || fail 'curl is required to download Go'
	mkdir -p -- "$toolchains"
	staging=$(mktemp -d -- "$toolchains/.go${version}-linux-${arch}.XXXXXXXXXX")
	trap 'rm -rf -- "$staging"' EXIT
	archive=go${version}.linux-${arch}.tar.gz
	url=https://go.dev/dl/$archive
	printf 'Downloading Go %s for linux/%s\n' "$version" "$arch" >&2
	curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
		--output "$staging/$archive" "$url"
	if ! (cd -- "$staging" && printf '%s  %s\n' "$checksum" "$archive" | sha256sum --check --status); then
		fail "SHA-256 verification failed for $archive"
	fi
	tar -xzf "$staging/$archive" -C "$staging"
	[[ -x $staging/go/bin/go ]] || fail "Go executable missing from $archive"

	# Another installer may have published the same verified toolchain first.
	if ! mv -T -- "$staging/go" "$cached" 2>/dev/null && [[ ! -x $cached/bin/go ]]; then
		fail "could not publish the Go toolchain at $cached"
	fi
	printf '%s\n' "$cached/bin/go"
)

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
	local arch go=go destination=${DESTDIR:-}/usr/local/bin
	[[ $(uname -s) == Linux ]] || fail 'make install targets Ubuntu Linux'
	case $(uname -m) in
	x86_64) arch=amd64 ;;
	aarch64) arch=arm64 ;;
	*) fail 'make install needs a linux/amd64 or linux/arm64 host' ;;
	esac

	if ! has_supported_go; then
		go=$(prepare_toolchain "$arch")
		unset GOROOT
	fi

	build_dir=$(mktemp -d /tmp/wgk-install.XXXXXXXXXX)
	trap 'rm -rf -- "$build_dir"' EXIT
	GOTOOLCHAIN=local GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
		"$go" build -mod=readonly -o "$build_dir/wgk" ./cmd/wgk

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
