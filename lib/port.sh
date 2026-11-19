#!/bin/bash

parse_port_argument() {
	local LC_ALL=C
	if [[ $# == 1 && $1 =~ ^0*([0-9]{1,5})$ ]]; then
		local port=$((10#${BASH_REMATCH[1]}))
		if ((port <= 65535)); then
			printf '%s\n' "$port"
			return 0
		fi
	fi

	printf 'Usage: ./%s <port> (decimal 0-65535)\n' "${0##*/}" >&2
	return 1
}
