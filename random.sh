#!/bin/bash

set -e

fail() {
	printf 'random.sh: %s\n' "$1" >&2
	exit 1
}

if (($# != 0)); then
	fail 'Usage: ./random.sh'
fi

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=lib/interface.sh
source "$script_dir/lib/interface.sh"
interface=$(resolve_interface "$script_dir/config/interface")
unit="wg-quick@${interface}.service"
config="/etc/wireguard/${interface}.conf"
change_helper="$script_dir/lib/port-change.py"

python3 -I "$change_helper" check "$unit"
if ! { exec 3<>/dev/tty; } 2>/dev/null; then
	fail 'confirmation requires a terminal'
fi
old=$("$script_dir/port.sh")

umask 077
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/wireguard-port.XXXXXXXXXX")
trap 'rm -rf -- "$work_dir"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

original="$work_dir/original"
candidate="$work_dir/candidate"
# Store the privileged read in the caller's private temporary directory.
# shellcheck disable=SC2024
sudo -- python3 -I "$change_helper" snapshot "$config" >"$original"

ports=$(shuf -i 10000-65535 -n 2)
new=${ports%%$'\n'*}
if [[ $new == "$old" ]]; then
	new=${ports##*$'\n'}
fi

python3 -I "$script_dir/lib/wg-config.py" replace "$original" "$old" "$new" >"$candidate"
original_hash=$(sha256sum <"$original")
candidate_hash=$(sha256sum <"$candidate")

printf 'Configuration %s\nListen port %s -> %s\n' "$config" "$old" "$new" >&3
diff_status=0
diff --color=auto -U0 --label "$config (before)" --label "$config (after)" -- "$original" "$candidate" >&3 || diff_status=$?
if ((diff_status != 1)); then
	fail 'could not preview the port change'
fi

printf '\nType %s to confirm the port change: ' "$new" >&3
if ! IFS= read -r answer <&3 || [[ $answer != "$new" ]]; then
	fail 'port change cancelled'
fi

backup=$(sudo -- python3 -I "$change_helper" commit "$unit" "$config" "$original" "$candidate" "${original_hash%% *}" "${candidate_hash%% *}")
printf 'Updated %s\nBackup %s\n' "$config" "$backup"
