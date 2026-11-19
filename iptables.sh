#!/bin/bash

script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 1
# shellcheck source=lib/port.sh
source "$script_dir/lib/port.sh" || exit 1
port=$(parse_port_argument "$@") || exit 1
exec sudo iptables -C INPUT -p udp --dport "$port" -j ACCEPT
