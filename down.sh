#!/bin/bash

if (($# != 0)); then
	printf 'Usage: ./down.sh\n' >&2
	exit 1
fi

script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 1
# shellcheck source=lib/interface.sh
source "$script_dir/lib/interface.sh" || exit 1
interface=$(resolve_interface "$script_dir/config/interface") || exit 1
exec sudo systemctl stop "wg-quick@${interface}.service"
