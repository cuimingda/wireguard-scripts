#!/bin/bash

resolve_listen_port() {
	local port_script=$1 port
	port=$("$port_script") || return "$?"

	local LC_ALL=C
	if [[ $port =~ ^[1-9][0-9]{4}$ ]] && ((port <= 65535)); then
		printf '%s\n' "$port"
		return 0
	fi

	printf '%s: port.sh must return a five-digit port (10000-65535)\n' "${0##*/}" >&2
	return 1
}
