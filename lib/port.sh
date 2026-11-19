#!/bin/bash

parse_port_argument() {
	local LC_ALL=C
	if [[ $# == 1 && $1 =~ ^[1-9][0-9]{4}$ ]] && (($1 <= 65535)); then
		printf '%s\n' "$1"
		return 0
	fi

	printf 'Usage: ./%s <port> (five digits, 10000-65535)\n' "${0##*/}" >&2
	return 1
}
