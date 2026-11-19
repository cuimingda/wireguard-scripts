# AGENTS.md

## Project Context

- This repository contains the Go and Cobra CLI `wgt` and standalone Bash scripts for maintaining WireGuard on Ubuntu servers with systemd. The service unit is `wg-quick@<interface>.service`.
- Use `make install` to install `wgt` and run `wgt status` from any directory. Run Bash scripts directly from the checkout, for example `./status.sh` or `./install.sh`. Keep Bash entry points in the repository root, use short operation names with a `.sh` extension, and track them as executable.
- `install.sh` is the upstream-derived interactive installer and management script. Preserve its existing behavior when adding maintenance scripts. Ubuntu with systemd is the target for new operations.

## Working Rules

- Check `git status --short --branch` and read the relevant scripts before editing. Use `rg` for search, keep diffs focused, and preserve existing user changes.
- When updating the checkout from its upstream, use `git pull --ff-only` before editing.
- Keep new scripts focused on one operation. Document each operator-facing invocation in `README.md` and use Bash syntax consistent with the existing scripts.
- Place shared Bash functions in `lib/`. Keep library files non-executable and source them relative to the entry point.

## Service and Configuration

- For service maintenance commands, resolve the interface before invoking `systemctl` and target its unit explicitly. Keep status queries read-only and scope service changes to the selected instance.
- `status.sh`, `port.sh`, `up.sh`, `down.sh`, and `random.sh` use `lib/interface.sh` to read a single interface name from the current user's `$XDG_CONFIG_HOME/wireguard-scripts/interface` when `XDG_CONFIG_HOME` is absolute, or `~/.config/wireguard-scripts/interface` otherwise. A missing, unreadable, or empty override falls back to `config/interface` in the checkout. The tracked default is `wg0`.
- Reuse the resolver for the command's language. `wgt` reads the same per-user file and has an embedded `wg0` fallback. Treat configuration as plain text and report invalid values on stderr before invoking the underlying command.
- `status.sh` takes no arguments. It prints `active` with exit code `0` for a loaded, active service and `inactive` with exit code `1` for a loaded, inactive service. Query failures and unexpected states use a distinct exit code with a diagnostic on stderr. Share the systemd state query with `random.sh`.
- `port.sh` takes no arguments and uses `sudo` and Python 3 to read the explicit `ListenPort` from `/etc/wireguard/<interface>.conf`.
- `udp.sh` and `iptables.sh` run without arguments and use `lib/port.sh` to get a five-digit decimal port from `10000` to `65535` by invoking the checkout's `port.sh`. Stop on lookup failure or invalid output. Preserve the native output and exit status of the socket query and `iptables -C` rule check.
- `random.sh` requires a loaded, inactive service. Read the old port through `port.sh` and verify it against the private configuration snapshot used for the preview. Require the operator to type the new port in the terminal and recheck the service and original configuration while committing. Keep the file change scoped to the previewed port and installer UDP hooks, with a private backup of the original.

## Validation

- For Go changes, run `gofmt` and `go test ./...`. For installation changes, verify `make install` on a disposable Ubuntu system and run `wgt status` from outside the checkout.
- For shell changes, run `bash -n` on each changed script and run the repository lint checks.

  ```bash
  shellcheck -e SC1091,SC1117,SC2001,SC2034 ./*.sh ./lib/*.sh
  shfmt -d ./*.sh ./lib/*.sh
  ```

- Verify behavior through direct `./<script>.sh` invocation. For service wrappers, use a temporary `HOME` and relevant stubs such as `systemctl` or `sudo` on `PATH` to check the selected interface, output, and exit status. Use a disposable Ubuntu environment with systemd when a check needs a real service manager.

## Completion Workflow

- Review `git status --short` and `git diff --check`. Stage the intended files and commit verified changes using Conventional Commits, for example `docs: add agent instructions`.
- If commitlint rejects the message, use its exact error to correct the message and retry.
