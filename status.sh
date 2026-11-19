#!/bin/bash

script_dir=$(dirname -- "${BASH_SOURCE[0]}") || exit 1
# shellcheck source=lib/interface.sh
source "$script_dir/lib/interface.sh" || exit 1
interface=$(resolve_interface "$script_dir/config/interface") || exit 1
exec systemctl is-active "wg-quick@${interface}.service"
