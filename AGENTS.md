# AGENTS.md

## Project Context

- This repository contains standalone Bash scripts for maintaining WireGuard on Ubuntu servers with systemd. The service unit is `wg-quick@<interface>.service`.
- Run scripts directly from the checkout, for example `./status.sh` or `./install.sh`. Keep entry points in the repository root, use short operation names with a `.sh` extension, and track them as executable. Command discovery stays within the checkout so it works independently of login profiles and global `PATH` setup.
- `install.sh` is the upstream-derived interactive installer and management script. Preserve its existing behavior when adding maintenance scripts. Ubuntu with systemd is the target for new operations.

## Working Rules

- Check `git status --short --branch` and read the relevant scripts before editing. Use `rg` for search, keep diffs focused, and preserve existing user changes.
- When updating the checkout from its upstream, use `git pull --ff-only` before editing.
- Keep new scripts focused on one operation. Document each operator-facing invocation in `README.md` and use Bash syntax consistent with the existing scripts.
- Place shared Bash functions in `lib/`. Keep library files non-executable and source them relative to the entry point.

## Service and Configuration

- For service maintenance commands, resolve the interface before invoking `systemctl` and target its unit explicitly. Keep status queries read-only and scope service changes to the selected instance.
- `status.sh`, `port.sh`, `up.sh`, and `down.sh` use `lib/interface.sh` to read a single interface name from the current user's `$XDG_CONFIG_HOME/wireguard-scripts/interface` when `XDG_CONFIG_HOME` is absolute, or `~/.config/wireguard-scripts/interface` otherwise. A missing, unreadable, or empty override falls back to `config/interface` in the checkout. The tracked default is `wg0`.
- Reuse this resolver for new interface commands. Treat configuration as plain text and report invalid values on stderr before invoking the underlying command.
- `status.sh` passes through `systemctl is-active` stdout, stderr, and exit status. Preserve this contract so a valid inactive result retains the underlying nonzero exit status.
- `port.sh` passes through `sudo wg show <interface> listen-port` stdout, stderr, and exit status for a single interface.
- `udp.sh` and `iptables.sh` take one decimal port from `0` to `65535` via `lib/port.sh`. Preserve the native output and exit status of the socket query and `iptables -C` rule check.

## Validation

- For shell changes, run `bash -n` on each changed script and run the repository lint checks.

  ```bash
  shellcheck -e SC1091,SC1117,SC2001,SC2034 ./*.sh ./lib/*.sh
  shfmt -d ./*.sh ./lib/*.sh
  ```

- Verify behavior through direct `./<script>.sh` invocation. For service wrappers, use a temporary `HOME` and relevant stubs such as `systemctl` or `sudo` on `PATH` to check the selected interface, output, and exit status. Use a disposable Ubuntu environment with systemd when a check needs a real service manager.

## Completion Workflow

- Review `git status --short` and `git diff --check`. Stage the intended files and commit verified changes using Conventional Commits, for example `docs: add agent instructions`.
- If commitlint rejects the message, use its exact error to correct the message and retry.
