#!/bin/bash

script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 1
if (($# == 2)) && [[ $1 == --config ]]; then
	exec python3 -I "$script_dir/lib/wg-config.py" port "$2"
fi
if (($# != 0)); then
	printf 'Usage: ./port.sh [--config <file>]\n' >&2
	exit 1
fi

# shellcheck source=lib/interface.sh
source "$script_dir/lib/interface.sh" || exit 1
interface=$(resolve_interface "$script_dir/config/interface") || exit 1

case $interface in
all | interfaces)
	printf 'port.sh: %s is reserved by wg show\n' "$interface" >&2
	exit 1
	;;
esac

exec sudo wg show "$interface" listen-port
