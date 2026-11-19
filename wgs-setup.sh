#!/bin/bash

set -e

fail() {
	printf 'wgs-setup.sh: %s\n' "$1" >&2
	exit 1
}

[[ -d ${HOME:-} ]] || fail 'HOME must be set to an existing directory'

script_dir=.
if [[ ${BASH_SOURCE[0]} == */* ]]; then
	script_dir=${BASH_SOURCE[0]%/*}
fi
# Keep trailing newlines in the captured path so they can be rejected below.
checkout=$(CDPATH='' cd -P -- "${script_dir:-/}" && printf '%s.' "$PWD")
checkout=${checkout%.}
if [[ $checkout == *:* || $checkout == *$'\n'* ]]; then
	fail 'the checkout path must not contain a colon or newline'
fi

login_shell=${SHELL:-/bin/bash}
profile=$HOME/.profile
case ${login_shell##*/} in
bash)
	# Bash reads only the first readable login profile.
	for candidate in "$HOME/.bash_profile" "$HOME/.bash_login"; do
		if [[ -f $candidate && -r $candidate ]]; then
			profile=$candidate
			break
		fi
	done
	;;
zsh) profile=${ZDOTDIR:-$HOME}/.zprofile ;;
sh | dash | ash) ;;
*) fail "unsupported login shell: $login_shell" ;;
esac

if [[ -e $profile && (! -f $profile || ! -r $profile) ]]; then
	fail "cannot read login profile: $profile"
fi

escaped_checkout=${checkout//\'/\'\\\'\'}
append_line="	*) export PATH=\"\${PATH:+\$PATH:}\"'$escaped_checkout' ;;"
path_block="case \":\${PATH-}:\" in
	*:'$escaped_checkout':*) ;;
$append_line
esac"

if [[ ! -f $profile ]] || ! grep -Fxq -- "$append_line" "$profile"; then
	printf '\n# Add the wireguard-scripts checkout to PATH.\n%s\n' "$path_block" >>"$profile"
fi

printf 'Configured PATH in %s for your next login.\n' "$profile"
printf 'To use the scripts in this session, run the following.\n%s\n' "$path_block"
