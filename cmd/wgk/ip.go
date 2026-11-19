package main

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type serverIP struct {
	interfaceName string
	address       netip.Addr
	scope         string
}

var (
	sharedIPv4 = netip.MustParsePrefix("100.64.0.0/10")
	globalIPv6 = netip.MustParsePrefix("2000::/3")

	// IANA special-purpose ranges within the ordinary unicast address space.
	// Private and shared addresses are handled separately.
	specialPurposeRanges = [...]netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.31.196.0/24"),
		netip.MustParsePrefix("192.52.193.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("192.175.48.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("2620:4f:8000::/48"),
		netip.MustParsePrefix("3fff::/20"),
	}
)

func newIPCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "ip",
		Short: "Inspect server IP addresses",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List public and private IPv4 and IPv6 addresses",
		Args:  cobra.NoArgs,
		RunE:  runIPList,
	})
	return command
}

func runIPList(cmd *cobra.Command, _ []string) error {
	addresses, err := listServerIPs()
	if err != nil {
		return err
	}
	output := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(output, "SCOPE\tFAMILY\tINTERFACE\tADDRESS"); err != nil {
		return err
	}
	for _, entry := range addresses {
		family := "IPv6"
		if entry.address.Is4() {
			family = "IPv4"
		}
		if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", entry.scope, family, entry.interfaceName, entry.address); err != nil {
			return err
		}
	}
	return output.Flush()
}

func listServerIPs() ([]serverIP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	var result []serverIP
	for _, iface := range interfaces {
		assigned, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses on %s: %w", iface.Name, err)
		}
		for _, item := range assigned {
			network, ok := item.(*net.IPNet)
			if !ok {
				continue
			}
			address, _ := netip.AddrFromSlice(network.IP)
			address = address.Unmap()
			if scope := serverIPScope(address); scope != "" {
				result = append(result, serverIP{interfaceName: iface.Name, address: address, scope: scope})
			}
		}
	}
	slices.SortFunc(result, func(a, b serverIP) int {
		if byInterface := strings.Compare(a.interfaceName, b.interfaceName); byInterface != 0 {
			return byInterface
		}
		return a.address.Compare(b.address)
	})
	return result, nil
}

func serverIPScope(address netip.Addr) string {
	if !address.IsGlobalUnicast() {
		return ""
	}
	if address.IsPrivate() || sharedIPv4.Contains(address) {
		return "private"
	}
	if address.Is6() && !globalIPv6.Contains(address) {
		return ""
	}
	for _, special := range specialPurposeRanges {
		if special.Contains(address) {
			return ""
		}
	}
	return "public"
}
