# AGENTS.md

## Project Context

- This repository contains the Go and Cobra CLI `wgk` for maintaining WireGuard on Ubuntu servers with systemd. The service unit is `wg-quick@<interface>.service`.
- Use `make install` to install `wgk`, then run `wgk service status` from any directory.
- Run the upstream-derived interactive installer and management script as `./install.sh` from the checkout. Keep it executable and preserve its existing behavior when changing `wgk` maintenance commands.

## Working Rules

- Check `git status --short --branch` and read the relevant code before editing. Use `rg` for search, keep diffs focused, and preserve existing user changes.
- When updating the checkout from its upstream, use `git pull --ff-only` before editing.
- Add WireGuard maintenance operations to `wgk` and document each operator-facing invocation in `README.md`. Follow the existing Bash style for installer changes.
- Place shared Bash functions in `lib/`. Keep library files non-executable and source them relative to the entry point.

## Service and Configuration

- Resolve the interface before invoking `systemctl` and target `wg-quick@<interface>.service` explicitly. Keep status queries read-only and scope service changes to the selected instance.
- `wgk` resolves the interface from the user YAML (`mingda.dev/wireguard-toolkit/config.yaml`), the legacy `wireguard-scripts/interface` file, then the embedded `wg0` default in `cmd/wgk/config/default.yaml`. Both user files are relative to an absolute `$XDG_CONFIG_HOME` or `~/.config`. `wgk config` manages the same YAML used by the maintenance commands. Treat interface names as plain text and report invalid values before invoking the underlying command.
- `wgk service status` takes no arguments. It prints `active` and exits `0` for a loaded, active service, or prints `inactive` and exits `1` for a loaded, inactive service. Query failures and unexpected states exit `2` with a diagnostic on stderr.
- `wgk service up` and `wgk service down` take no arguments. They start or stop the selected unit with `systemctl`, using `sudo` for non-root users and passing through the child process's exit status and terminal I/O.
- `wgk port get` reads the explicit `[Interface]` `ListenPort` from the file shown by `wgk service config path`. `wgk port generate` reads the same setting and prints a random port from `10000` to `65535` that differs from the current port.
- `wgk port udp` and `wgk port iptables` call `wgk port get` and validate the five-digit port from `10000` to `65535`. They pass through the native socket query and `iptables -C` rule check's output and exit status.
- `wgk port set <port>` requires a loaded, inactive service and previews changes to `ListenPort` and matching installer UDP hooks. Enter accepts the preview and `n` declines. `--force` (`-f`) skips confirmation. Recheck the systemd state and original configuration before atomically replacing the file.
- `wgk random` resolves one interface, generates a different port, stops the selected service, applies the port change with the same behavior as `wgk port set --force`, and starts that service. If a step fails, stop the sequence.

## Validation

- For Go changes, run `gofmt` and `go test ./...`. For installation changes, verify `make install` on a disposable Ubuntu system and run `wgk service status` from outside the checkout.
- For shell changes, run `bash -n` on each changed script and run the repository lint checks.

  ```bash
  shellcheck -e SC1091,SC1117,SC2001,SC2034 ./*.sh ./lib/*.sh
  shfmt -d ./*.sh ./lib/*.sh
  ```

- Verify changes through the relevant `wgk` command or direct installer invocation. For service commands, use a temporary configuration home and stubs such as `systemctl` or `sudo` on `PATH` to check the selected interface, output, and exit status. Use a disposable Ubuntu environment with systemd when a check needs a real service manager.

## Completion Workflow

- Review `git status --short` and `git diff --check`. Stage the intended files and commit verified changes using Conventional Commits, for example `docs: add agent instructions`.
- If commitlint rejects the message, use its exact error to correct the message and retry.
