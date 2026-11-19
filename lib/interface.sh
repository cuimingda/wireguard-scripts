#!/bin/bash

config_error() {
	printf '%s: %s: %s\n' "${0##*/}" "$1" "$2" >&2
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
	printf '%s\n' "$value"
}

resolve_interface() {
	local default_config=$1
	local config_home=${XDG_CONFIG_HOME:-}
	if [[ $config_home != /* ]]; then
		config_home=${HOME:+$HOME/.config}
	fi

	if [[ -n $config_home ]] && read_interface "$config_home/wireguard-scripts/interface"; then
		return 0
	fi
	read_interface "$default_config" || config_error "$default_config" 'could not read a default interface'
}
