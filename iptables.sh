#!/bin/bash

if (($# != 0)); then
	printf 'Usage: ./iptables.sh\n' >&2
	exit 1
fi

script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 1
# shellcheck source=lib/port.sh
source "$script_dir/lib/port.sh" || exit 1
port=$(resolve_listen_port "$script_dir/port.sh") || exit "$?"
exec sudo iptables -C INPUT -p udp --dport "$port" -j ACCEPT
