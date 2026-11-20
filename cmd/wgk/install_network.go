package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

const (
	installForwardingName = "90-wgk-wireguard.conf"
	installForwarding     = "net.ipv4.ip_forward = 1\nnet.ipv6.conf.all.forwarding = 1\n"
)

type installRoute struct {
	Device string     `json:"dev"`
	Source netip.Addr `json:"prefsrc"`
}

func checkExistingInstallLinks(cmd *cobra.Command, name string) error {
	output, err := installCommandOutput(cmd, "ip", "-j", "-d", "link", "show")
	if err != nil {
		return fmt.Errorf("check for existing WireGuard interfaces: %w", err)
	}
	var links []struct {
		Name string `json:"ifname"`
		Info struct {
			Kind string `json:"info_kind"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal(output, &links); err != nil {
		return fmt.Errorf("parse network interfaces: %w", err)
	}
	for _, link := range links {
		if link.Name == name || link.Info.Kind == "wireguard" {
			return fmt.Errorf("network interface %s already exists", link.Name)
		}
	}
	return nil
}

func discoverInstallNetwork(cmd *cobra.Command, name string) (installState, error) {
	// The route lookups only query the kernel. No packets are sent.
	ipv4, err := lookupInstallRoute(cmd, name, "-4", "1.1.1.1")
	if err != nil {
		return installState{}, fmt.Errorf("find public IPv4: %w", err)
	}
	endpoint, err := installPublicIPv4(ipv4)
	if err != nil {
		return installState{}, err
	}
	ipv6, err := lookupInstallRoute(cmd, name, "-6", "2606:4700:4700::1111")
	if err != nil {
		return installState{}, fmt.Errorf("find public IPv6 uplink: %w", err)
	}
	if !ipv6.Source.Is6() || serverIPScope(ipv6.Source) != "public" {
		return installState{}, fmt.Errorf("no public IPv6 source address on %s", ipv6.Device)
	}
	return installState{Interface: name, Endpoint: endpoint.String(), IPv4Uplink: ipv4.Device, IPv6Uplink: ipv6.Device}, nil
}

func lookupInstallRoute(cmd *cobra.Command, name, family, destination string) (installRoute, error) {
	output, err := installCommandOutput(cmd, "ip", "-j", family, "route", "get", destination)
	if err != nil {
		return installRoute{}, fmt.Errorf("query %s route: %w", family, err)
	}
	var routes []installRoute
	if err := json.Unmarshal(output, &routes); err != nil {
		return installRoute{}, fmt.Errorf("parse %s route: %w", family, err)
	}
	if len(routes) != 1 || !validInstallUplink(routes[0].Device, name) {
		return installRoute{}, fmt.Errorf("no usable %s uplink", family)
	}
	return routes[0], nil
}

func validInstallUplink(uplink, name string) bool {
	return validInterfaceName(uplink) && uplink != name && uplink != "lo"
}

func installPublicIPv4(route installRoute) (netip.Addr, error) {
	if route.Source.Is4() && serverIPScope(route.Source) == "public" {
		return route.Source, nil
	}
	addresses, err := listServerIPs()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("find public IPv4: %w", err)
	}
	var public netip.Addr
	for _, address := range addresses {
		if address.interfaceName != route.Device || !address.address.Is4() || address.scope != "public" {
			continue
		}
		if public.IsValid() && public != address.address {
			return netip.Addr{}, fmt.Errorf("cannot choose a public IPv4 address on %s", route.Device)
		}
		public = address.address
	}
	if !public.IsValid() {
		return netip.Addr{}, fmt.Errorf("no public IPv4 address on %s", route.Device)
	}
	return public, nil
}

func applyInstallForwarding(cmd *cobra.Command, paths installPaths, state installState, pending bool) error {
	path := filepath.Join(paths.sysctlDir, installForwardingName)
	raSetting := fmt.Sprintf("net/ipv6/conf/%s/accept_ra = 2\n", state.IPv6Uplink)
	existing, err := readInstallFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Preserve kernel-managed router advertisements when forwarding is enabled.
		raPath := filepath.Join(paths.procSysDir, "net/ipv6/conf", state.IPv6Uplink, "accept_ra")
		ra, err := os.ReadFile(raPath)
		if err != nil {
			return fmt.Errorf("read the IPv6 uplink's router advertisement setting: %w", err)
		}
		config := installForwarding
		switch strings.TrimSpace(string(ra)) {
		case "0":
		case "1", "2":
			config = raSetting + config
		default:
			return fmt.Errorf("unexpected IPv6 router advertisement setting in %s", raPath)
		}
		if err := writeUserConfig(path, []byte(config), false); err != nil {
			return fmt.Errorf("write WireGuard forwarding configuration %s: %w", path, err)
		}
	} else if err != nil {
		return err
	} else if !pending || (!bytes.Equal(existing, []byte(installForwarding)) && !bytes.Equal(existing, []byte(raSetting+installForwarding))) {
		return fmt.Errorf("existing forwarding configuration at %s", path)
	}
	if err := runInstallProcess(cmd, "sysctl", "-p", path); err != nil {
		return fmt.Errorf("apply WireGuard forwarding settings: %w", err)
	}
	return nil
}
