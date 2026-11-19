#!/bin/bash

if (($# != 0)); then
	printf 'Usage: ./status.sh\n' >&2
	exit 2
fi

script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 2
# shellcheck source=lib/interface.sh
source "$script_dir/lib/interface.sh" || exit 2
interface=$(resolve_interface "$script_dir/config/interface") || exit 2
if ! command -v python3 >/dev/null; then
	printf 'status.sh: python3 is required\n' >&2
	exit 2
fi
exec python3 -I "$script_dir/lib/port-change.py" status "wg-quick@${interface}.service"
