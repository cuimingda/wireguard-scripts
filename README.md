# WireGuard Toolkit

![Lint](https://github.com/angristan/wireguard-install/workflows/Lint/badge.svg)
[![Say Thanks!](https://img.shields.io/badge/Say%20Thanks-!-1EAEDB.svg)](https://saythanks.io/to/angristan)

Use `wgk` to set up and manage [WireGuard](https://www.wireguard.com/) on Ubuntu. The interactive `install.sh` also supports other Linux distributions.

WireGuard is a point-to-point VPN that can be used in different ways. Here, we mean a VPN as in: the client will forward all its traffic through an encrypted tunnel to the server.
The server will apply NAT to the client's traffic so it will appear as if the client is browsing the web with the server's IP.

The script supports both IPv4 and IPv6. Please check the [issues](https://github.com/angristan/wireguard-install/issues) for ongoing development, bugs and planned features! You might also want to check the [discussions](https://github.com/angristan/wireguard-install/discussions) for help.

WireGuard does not fit your environment? Check out [openvpn-install](https://github.com/angristan/openvpn-install).

## Requirements

Every `wgk` invocation, including `wgk --help`, requires Ubuntu Linux. At startup, `wgk` checks for `ID=ubuntu` in `/etc/os-release` or, if that file is missing, `/usr/lib/os-release`. If it cannot confirm Ubuntu, it prints a diagnostic to stderr and exits with code `2`.

Distributions supported by `install.sh`:

- AlmaLinux >= 8
- Alpine Linux
- Arch Linux
- CentOS Stream >= 8
- Debian >= 10
- Fedora >= 32
- Oracle Linux
- Rocky Linux >= 8
- Ubuntu >= 18.04

## Usage

For the interactive installer, clone this repository on the server and run it from the checkout. Answer the prompts to configure WireGuard.

```bash
./install.sh
```

It will install WireGuard (kernel module and tools) on the server, configure it, create a systemd service and a client configuration file.

Run the script again to add or remove clients!

### Install `wgk` for the first time

After cloning the repository on Ubuntu (`amd64` or `arm64`), run this from the checkout.

```bash
./setup.sh
```

By default, if `/usr/local/bin/wgk` already exists as an executable file, setup exits successfully before running APT or Make. Otherwise it installs `make`, `golang-1.26-go`, and `ca-certificates` through APT and runs `make install`. If APT cannot find the Go package on Ubuntu 22.04 or 24.04, setup tries adding the official `updates` source for the `universe` component. Installing prerequisites requires root or `sudo` access.

To run the APT dependency installation and `make install` again even when `wgk` is already executable, run this command.

```bash
./setup.sh --force
```

### Rebuild and reinstall `wgk`

From the checkout, run `make install` whenever you want to build and install again. To update from upstream first, run these commands.

```bash
git pull --ff-only
make install
```

`make install` builds with `GOTOOLCHAIN=local` and uses `/usr/lib/go-1.26/bin/go` by default. To use another installed Go 1.26.0 or newer, run `make install GO=/path/to/go`. The build may need HTTPS access to fetch Go modules. Installation replaces `/usr/local/bin/wgk` after a successful build and requires write access or `sudo`. If `DESTDIR` is set, it prefixes the target path used by both `make install` and `./setup.sh`.

### Configure WireGuard on a new Ubuntu server

```bash
wgk install
```

Run this once on an Ubuntu server with systemd and public IPv4 and IPv6 connectivity. The first installation reads the same `interface` setting as `wgk service` and `wgk port`. It defaults to `wg0`. Set a custom interface with `wgk config set interface <name>` before installing.

`wgk` selects the server's public IPv4 as its endpoint and exits with an error if it cannot find one. It installs missing WireGuard runtime tools via APT, creates `/etc/wireguard/<interface>.conf` with a fresh server key, and enables forwarding, NAT, and inbound UDP for both address families. It uses `wgk port generate` to select a UDP port from `10000` to `65535` when creating the configuration. The default tunnel addresses are `10.66.66.1/24` and `fd42:42:42::1/64`. The command uses `sudo` when run by a non-root user.

Before deploying the service, `wgk install` checks the public IPv4 and IPv6 routes, tunnel subnet conflicts, required tools, systemd, forwarding settings, firewall rules, and availability of the chosen UDP port on both address families. Missing packages are prepared before the checks that use them.

After a failure or interruption, rerun `wgk install` to clean up the previous attempt's tracked service, files, and rules. It then checks the server again using the current interface and network settings, and generates a fresh key and port. It stops if it cannot establish ownership of a conflicting file or interface. An older unfinished installation may lack records of the original kernel forwarding values. In that case the installer reports that it is keeping the live values.

`wgk` records success after verifying that `wg-quick@<interface>.service` is active, enabled at boot, and listening on the configured port, and that dual-stack forwarding, routes, and firewall rules are in place. Repeating `wgk install` after completion prints the installed interface and exits successfully, even if the `interface` setting has since changed.

### Inspect the build and runtime environment

Run this from any directory on Ubuntu to capture a diagnostic snapshot.

```bash
wgk env
```

The output is sorted by key, with one `key=value` per line. `host.*` reports the Ubuntu release, kernel, machine architecture, available CPUs, and systemd boot state. `build.*` reports the compiled binary's Go version, target `GOOS` and `GOARCH`, CPU feature level, CGO setting, module versions, and Git build metadata when available. `tool.go.*` reports Go found on `PATH`, while `tool.build_go.*` reports the toolchain selected by the exported `GO` variable or the Makefile default.

`runtime.*` reports the executable, working directory, user IDs, and selected WireGuard interface, paths, and service unit. `tool.*` also shows the paths and available versions of build and WireGuard tools in the current environment. Missing paths and failed version probes are blank. `env.*` includes relevant exported variables. Proxy, authentication, and raw Go flag variables report only whether they are set. Control characters are escaped to keep each record on one line. The command runs without `sudo` and works before WireGuard is configured.

### Check whether the service is active

Once installed, run this from any directory on Ubuntu with systemd as your normal user.

```bash
wgk service status
```

It prints `active` and exits `0` for a running service, or prints `inactive` and exits `1` for a stopped service. Query errors and unexpected service states exit `2` with a diagnostic on stderr.

### List server IP addresses

```bash
wgk ip list
```

This lists the public and private IPv4 and IPv6 addresses assigned to local interfaces. Each row shows the scope, address family, interface, and address. The command runs without `sudo` and determines scope from the address range. Private addresses include IPv4 shared address space (`100.64.0.0/10`) and IPv6 unique local addresses. The list omits loopback, link-local, and other special-purpose addresses. A server with only private addresses can still be listed successfully.

### Start or stop the service

After installing `wgk`, run `wgk service up` to start the configured WireGuard service or `wgk service down` to stop it.

```bash
wgk service up
wgk service down
```

Both commands run `systemctl` directly as root and use `sudo` for other users. They pass through the underlying command's output and exit status.

### Show the WireGuard configuration path

```bash
wgk service config path
```

This prints `/etc/wireguard/wg0.conf` for the default interface. It uses the same effective interface setting as `wgk service status` and can be run before the WireGuard configuration file is created.

### Show the listen port

```bash
wgk port get
```

This prints the explicit `ListenPort` in the `[Interface]` section of the file selected by `wgk service config path`. It works while the service is running or stopped. If reading the file requires root access, `wgk` uses `sudo` for the read.

### Check a UDP port

```bash
wgk port udp
wgk port iptables
```

Both commands call `wgk port get` and require a five-digit port from `10000` to `65535`. `wgk port udp` uses `ss` to list matching local UDP sockets. `wgk port iptables` uses `iptables -C` to check for a matching IPv4 UDP `ACCEPT` rule in the `INPUT` chain, including the rule created by `wgk install`. They pass through the underlying command's stdout, stderr, and exit status.

### Generate a random listen port

```bash
wgk port generate
```

This reads the same configured `ListenPort` as `wgk port get` and prints one random port from `10000` to `65535` that differs from it. The command only reads the configuration and works while the service is running or stopped.

### Change the listen port

To set a specific five-digit port from `10000` to `65535`, stop the configured service and run the change in a terminal.

```bash
wgk service down
wgk port set 51821
```

`wgk port set` requires a loaded, inactive service. It previews the old and new values of `ListenPort` and matching UDP hooks generated by the installer in the file shown by `wgk service config path`. At the `[Y/n]` prompt, press Enter or type `y` to apply the change. Type `n` to decline. End-of-input without a response cancels the change.

The command uses `sudo` for non-root users and checks the service and configuration again before replacing the file. Run `wgk service up` when you are ready to start the service.

Use `--force` (`-f`) on `wgk port set` to print the preview and apply the change without a confirmation prompt.

```bash
wgk port set --force 51821
```

The service must still be loaded and inactive. For non-root use without a terminal, `sudo` must already be able to authorize the command without prompting.

### Change to a random listen port

```bash
wgk random
```

`wgk random` generates a port different from the current one, stops the configured service, applies the change with `port set --force`, and starts the same service. It prints the preview and applies it without prompting. A failed step stops the sequence before any later steps run. For non-root use without a terminal, `sudo` must already be able to authorize the commands without prompting.

After changing the port, update the client endpoints separately. The interactive `install.sh` gets the endpoint port for future clients from `/etc/wireguard/params`. Existing clients use the `Endpoint` in their own configuration files.

### Select the interface

`wgk` embeds its defaults from `cmd/wgk/config/default.yaml` at build time and overlays the user YAML at runtime. The default interface is `wg0`. Set a user override and check the resolved value with these commands.

```bash
wgk config set interface wg1
wgk config get interface
```

The user YAML file is `~/.config/wireguard-scripts/config.yaml`. An absolute `$XDG_CONFIG_HOME` replaces `~/.config`. `wgk config path` returns the full path, and `wgk config edit --editor nano` opens the file. The default editor is `vi`. `wgk config init` creates the file from the embedded defaults.

Use `wgk config list` for the resolved configuration, `wgk config list --default` for all settings from the embedded defaults, and `wgk config list --user` for user YAML values. Use `--filter interface` to show only that key. The list prints sorted `key=value` lines such as `interface=wg0`. Nested keys use dots, as in `network.retries=3`, and sequences use inline YAML.

`wgk config get` prints the value. `path`, `init`, and `edit` print the absolute configuration path. `wgk config unset interface` removes the YAML override. `set` and `unset` complete silently on success. Configuration commands exit with `0` on success or `1` with a diagnostic on stderr when an operation fails.

When upgrading, copy any existing YAML overrides you want to keep to the path returned by `wgk config path`. To carry over a custom interface alone, use `wgk config set interface <name>`.

## Providers

I recommend these cheap cloud providers for your VPN server:

- [Vultr](https://www.vultr.com/?ref=8948982-8H): Worldwide locations, IPv6 support, starting at \$5/month
- [Hetzner](https://hetzner.cloud/?ref=ywtlvZsjgeDq): Germany, Finland and USA. IPv6, 20 TB of traffic, starting at 4.5€/month
- [Digital Ocean](https://m.do.co/c/ed0ba143fe53): Worldwide locations, IPv6 support, starting at \$4/month

## Contributing

Contributions are welcome! Here's how you can help:

### Discuss changes

Please open an issue before submitting a PR if you want to discuss a change, especially if it's a big one.

### Code formatting

Format Go changes with `gofmt` and run `go test ./...`.

We use [shellcheck](https://github.com/koalaman/shellcheck) and [shfmt](https://github.com/mvdan/sh) to enforce bash styling guidelines and good practices. They are executed for each commit / PR with GitHub Actions, so you can check the configuration [here](https://github.com/angristan/wireguard-install/blob/master/.github/workflows/lint.yml).

## Say thanks

You can [say thanks](https://saythanks.io/to/angristan) if you want!

## Credits & Licence

This project is under the [MIT Licence](https://raw.githubusercontent.com/angristan/wireguard-install/master/LICENSE)

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=angristan/wireguard-install&type=Date)](https://star-history.com/#angristan/wireguard-install&Date)
