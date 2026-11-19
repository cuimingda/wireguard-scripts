#!/bin/bash

if (($# != 0)); then
	printf 'Usage: ./port.sh\n' >&2
	exit 1
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P) || exit 1
# shellcheck source=lib/interface.sh
source "$script_dir/lib/interface.sh" || exit 1
interface=$(resolve_interface "$script_dir/config/interface") || exit 1
exec sudo -- python3 -I "$script_dir/lib/wg-config.py" port "/etc/wireguard/${interface}.conf"
