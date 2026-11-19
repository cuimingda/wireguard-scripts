#!/bin/bash

config_error() {
	printf 'wgs-is-active.sh: %s: %s\n' "$1" "$2" >&2
	exit 1
}

read_interface() {
	local file=$1 value=''
	local LC_ALL=C

	[[ -f $file ]] || return 1

	# Read the entire file. A successful read means a NUL was found.
	if IFS= read -r -d '' value 2>/dev/null <"$file"; then
		config_error "$file" 'invalid interface name'
	fi
	value=${value%$'\n'}
	[[ -n $value ]] || return 1

	if [[ ! $value =~ ^[A-Za-z0-9_.-]{1,15}$ || $value == . || $value == .. ]]; then
		config_error "$file" 'invalid interface name'
	fi
	interface=$value
}

config_home=${XDG_CONFIG_HOME:-}
if [[ $config_home != /* ]]; then
	config_home=${HOME:+$HOME/.config}
fi

if [[ -z $config_home ]] || ! read_interface "$config_home/wireguard-scripts/interface"; then
	script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 1
	default_config=$script_dir/config/interface
	if ! read_interface "$default_config"; then
		config_error "$default_config" 'could not read a default interface'
	fi
fi

exec systemctl is-active "wg-quick@${interface}.service"
