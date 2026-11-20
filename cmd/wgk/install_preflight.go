package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var installRequiredTools = [...]string{"wg", "wg-quick", "iptables", "ip6tables", "iptables-restore", "ip6tables-restore"}

func preflightInstallHost(cmd *cobra.Command, paths installPaths, name string, settings installSettings) (installState, error) {
	for _, dir := range []string{paths.wireGuardDir, paths.sysctlDir} {
		if err := checkInstallDirectory(dir); err != nil {
			return installState{}, err
		}
	}
	if err := checkExistingInstall(paths.wireGuardDir); err != nil {
		return installState{}, err
	}
	if err := checkInstallBaseTools(); err != nil {
		return installState{}, err
	}
	if info, err := os.Stat(paths.systemdDir); err != nil || !info.IsDir() {
		return installState{}, errors.New("WireGuard installation requires a running systemd")
	}
	version, err := installCommandOutput(cmd, "systemctl", "show", "--property=Version", "--value")
	if err != nil {
		return installState{}, fmt.Errorf("connect to systemd: %w", err)
	}
	if strings.TrimSpace(string(version)) == "" {
		return installState{}, errors.New("systemd returned an empty version")
	}
	if err := checkUnusedInstallTargets(cmd, paths, name); err != nil {
		return installState{}, err
	}
	state, err := discoverInstallNetwork(cmd, name)
	if err != nil {
		return installState{}, err
	}
	if err := checkInstallRouteOverlap(cmd, "-4", settings.IPv4); err != nil {
		return installState{}, err
	}
	if err := checkInstallRouteOverlap(cmd, "-6", settings.IPv6); err != nil {
		return installState{}, err
	}
	return state, nil
}

func checkInstallDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect installation directory %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("expected a trusted installation directory at %s", path)
	}
	if err := unix.Access(path, unix.R_OK|unix.W_OK|unix.X_OK); err != nil {
		return fmt.Errorf("access installation directory %s: %w", path, err)
	}
	return nil
}

func checkInstallBaseTools() error {
	for _, tool := range []string{"ip", "sysctl", "systemctl"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required installation tool %s is unavailable: %w", tool, err)
		}
	}
	for _, tool := range installRequiredTools {
		if _, err := exec.LookPath(tool); err == nil {
			continue
		}
		if _, err := exec.LookPath("apt-get"); err != nil {
			return fmt.Errorf("apt-get is required to install the missing WireGuard tool %s: %w", tool, err)
		}
		break
	}
	return nil
}

func checkInstallRouteOverlap(cmd *cobra.Command, family string, tunnel netip.Prefix) error {
	output, err := installCommandOutput(cmd, "ip", "-j", family, "route", "show", "table", "all")
	if err != nil {
		return fmt.Errorf("list %s routes for WireGuard installation: %w", family, err)
	}
	var routes []struct {
		Destination string `json:"dst"`
		Device      string `json:"dev"`
	}
	if err := json.Unmarshal(output, &routes); err != nil {
		return fmt.Errorf("parse %s routes for WireGuard installation: %w", family, err)
	}
	for _, route := range routes {
		if route.Destination == "" || route.Destination == "default" {
			continue
		}
		prefix, err := netip.ParsePrefix(route.Destination)
		if err != nil {
			address, addrErr := netip.ParseAddr(route.Destination)
			if addrErr != nil {
				return fmt.Errorf("parse %s route destination %q: %w", family, route.Destination, err)
			}
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		if prefix.Bits() == 0 || !tunnel.Overlaps(prefix) {
			continue
		}
		if route.Device == "" {
			return fmt.Errorf("WireGuard subnet %s overlaps existing route %s", tunnel.Masked(), route.Destination)
		}
		return fmt.Errorf("WireGuard subnet %s overlaps existing route %s on %s", tunnel.Masked(), route.Destination, route.Device)
	}
	return nil
}
