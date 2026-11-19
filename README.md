# WireGuard Toolkit

![Lint](https://github.com/angristan/wireguard-install/workflows/Lint/badge.svg)
[![Say Thanks!](https://img.shields.io/badge/Say%20Thanks-!-1EAEDB.svg)](https://saythanks.io/to/angristan)

Use `wgk` to manage WireGuard services on Ubuntu and `install.sh` to set up a [WireGuard](https://www.wireguard.com/) VPN on Linux.

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

Clone this repository on the server and run the installer from the checkout. Answer the prompts to configure WireGuard.

```bash
./install.sh
```

It will install WireGuard (kernel module and tools) on the server, configure it, create a systemd service and a client configuration file.

Run the script again to add or remove clients!

### Check whether the service is active

Install Make on Ubuntu with systemd (`amd64` or `arm64`), then run these commands from your existing checkout.

```bash
git pull
make install
wgk service status
```

`make install` builds `wgk` from the checkout and installs it in `/usr/local/bin`. It uses Go 1.26.0 or newer when available. When it needs to download Go, it uses `curl` over HTTPS, verifies the pinned toolchain's SHA-256 checksum, and caches it for the current user. The first build needs HTTPS access to the Go module servers. Installing to `/usr/local/bin` requires write access or `sudo`.

Once installed, run `wgk service status` from any directory as your normal user. It prints `active` and exits `0` for a running service, or prints `inactive` and exits `1` for a stopped service. Query errors and unexpected service states exit `2` with a diagnostic on stderr.

The checkout also provides a Bash entry point that uses Python 3.

```bash
./status.sh
```

### Start or stop the service

After installing `wgk`, run `wgk service up` to start the configured WireGuard service or `wgk service down` to stop it.

```bash
wgk service up
wgk service down
```

Both commands run `systemctl` directly as root and use `sudo` for other users. They pass through the underlying command's output and exit status.

The checkout also provides the corresponding Bash entry points.

```bash
./up.sh
./down.sh
```

These scripts run `sudo systemctl start` and `sudo systemctl stop` for the configured WireGuard service.

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

The checkout also provides a Bash entry point that uses `sudo` and Python 3.

```bash
./port.sh
```

### Check a UDP port

```bash
wgk port udp
wgk port iptables
```

Both commands call `wgk port get` and require a five-digit port from `10000` to `65535`. `wgk port udp` uses `ss` to list matching local UDP sockets. `wgk port iptables` uses `iptables -C` to check for the UDP `ACCEPT` rule in the `INPUT` chain. They pass through the underlying command's stdout, stderr, and exit status.

The checkout also provides the corresponding Bash entry points.

```bash
./udp.sh
./iptables.sh
```

Both scripts run without arguments and call `port.sh` to get the configured WireGuard port from disk. The returned port must be a five-digit decimal number from `10000` to `65535`.

`udp.sh` lists matching local UDP sockets using `ss`. `iptables.sh` uses `iptables -C` to check for a matching UDP `ACCEPT` rule in the `INPUT` chain. Each check passes through its underlying command's output and exit code.

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

Run `wgk random` in a terminal without arguments. It checks that a terminal is available before stopping the service. It generates a port different from the current one, stops the configured service, previews the change and prompts for confirmation, and starts the same service. If you decline at the prompt, it starts the service with its original port. A failed step stops the sequence before any later steps run.

The checkout also provides `./random.sh`. Stop the configured service with `./down.sh`, then run `./random.sh` in a terminal. It uses Python 3 and requires the systemd unit to be loaded and inactive.

Review the interface configuration path, the old and new ports, and the diff. The changes cover `ListenPort` and matching UDP firewall hooks generated by the installer. Type the displayed new port to apply the previewed change. If the service state or configuration changes during confirmation, rerun the command to get a fresh preview. A successful change prints the path of the backup alongside the original configuration.

Use `./up.sh` when you are ready to start the service.

After changing the port, update the client endpoints separately. The installer gets the endpoint port for future clients from `/etc/wireguard/params`. Existing clients use the `Endpoint` in their own configuration files.

### Select the interface

The `wgk service` commands use the `interface` setting from an embedded default configuration. The default is `wg0`. Set a user override and check the resolved value with these commands.

```bash
wgk config set interface wg1
wgk config get interface
```

The user YAML file is `~/.config/mingda.dev/wireguard-toolkit/config.yaml`. An absolute `$XDG_CONFIG_HOME` replaces `~/.config`. `wgk config path` returns the full path, and `wgk config edit --editor nano` opens the file. The default editor is `vi`. `wgk config init` creates the file from the defaults, seeding it with the existing interface selection when upgrading from a legacy interface file.

Use `wgk config list` for the resolved configuration, `wgk config list --default` for the embedded defaults, and `wgk config list --user` for user YAML values. Use `--filter interface` to show only that key. The list prints sorted `key=value` lines such as `interface=wg0`. Nested keys use dots, as in `network.retries=3`, and sequences use inline YAML.

`wgk config get` prints the value. `path`, `init`, and `edit` print the absolute configuration path. `wgk config unset interface` removes the YAML override. `set` and `unset` complete silently on success. Configuration commands exit with `0` on success or `1` with a diagnostic on stderr when an operation fails.

When the user YAML has no `interface` key, `wgk` checks the legacy per-user `wireguard-scripts/interface` file before using the embedded default. The Bash commands read the legacy file and fall back to `config/interface` in the checkout. To select `wg1` for the Bash commands, write the legacy file.

```bash
mkdir -p "$HOME/.config/wireguard-scripts"
printf '%s\n' wg1 >"$HOME/.config/wireguard-scripts/interface"
```

Use an absolute `$XDG_CONFIG_HOME` in place of `$HOME/.config` here as well. A missing, unreadable, or empty legacy file falls back to the default. The legacy files each contain a single interface name.

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
