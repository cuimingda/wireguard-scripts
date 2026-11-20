package main

import (
	"encoding/json"
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

type installLink struct {
	Name string `json:"ifname"`
	Info struct {
		Kind string `json:"info_kind"`
	} `json:"linkinfo"`
}

func readInstallLinks(cmd *cobra.Command) ([]installLink, error) {
	output, err := installCommandOutput(cmd, "ip", "-j", "-d", "link", "show")
	if err != nil {
		return nil, fmt.Errorf("check for existing WireGuard interfaces: %w", err)
	}
	var links []installLink
	if err := json.Unmarshal(output, &links); err != nil {
		return nil, fmt.Errorf("parse network interfaces: %w", err)
	}
	return links, nil
}

func checkExistingInstallLinks(cmd *cobra.Command, name string) error {
	links, err := readInstallLinks(cmd)
	if err != nil {
		return err
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

type installSysctl struct {
	Key    string `yaml:"key"`
	Before string `yaml:"before"`
	After  string `yaml:"after"`
}

func planInstallForwarding(paths installPaths, state installState) ([]installSysctl, error) {
	// Apply accept_ra before changing global IPv6 forwarding to keep the uplink route.
	raKey := "net/ipv6/conf/" + state.IPv6Uplink + "/accept_ra"
	ra, err := readInstallSysctl(paths, raKey)
	if err != nil {
		return nil, err
	}
	var settings []installSysctl
	switch ra {
	case "0":
	case "1", "2":
		settings = append(settings, installSysctl{Key: raKey, Before: ra, After: "2"})
	default:
		return nil, fmt.Errorf("unexpected value %q for %s", ra, raKey)
	}
	for _, key := range []string{"net/ipv4/ip_forward", "net/ipv6/conf/all/forwarding"} {
		before, err := readInstallSysctl(paths, key)
		if err != nil {
			return nil, err
		}
		if before != "0" && before != "1" {
			return nil, fmt.Errorf("unexpected value %q for %s", before, key)
		}
		settings = append(settings, installSysctl{Key: key, Before: before, After: "1"})
	}
	for _, setting := range settings {
		path := filepath.Join(paths.procSysDir, setting.Key)
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return nil, fmt.Errorf("open sysctl %s for writing: %w", setting.Key, err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("close sysctl %s: %w", setting.Key, err)
		}
	}
	return settings, nil
}

func readInstallSysctl(paths installPaths, key string) (string, error) {
	value, err := os.ReadFile(filepath.Join(paths.procSysDir, key))
	if err != nil {
		return "", fmt.Errorf("read sysctl %s: %w", key, err)
	}
	return strings.TrimSpace(string(value)), nil
}

func renderInstallForwarding(settings []installSysctl) []byte {
	var config strings.Builder
	for _, setting := range settings {
		fmt.Fprintf(&config, "%s = %s\n", setting.Key, setting.After)
	}
	return []byte(config.String())
}

func verifyInstallForwarding(paths installPaths, settings []installSysctl) error {
	for _, setting := range settings {
		current, err := readInstallSysctl(paths, setting.Key)
		if err != nil {
			return err
		}
		if current != setting.After {
			return fmt.Errorf("verify sysctl %s: expected %s, got %s", setting.Key, setting.After, current)
		}
	}
	return nil
}
