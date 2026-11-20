package main

import (
	"fmt"
	"net/netip"
	"strings"
)

const installFirewallOwnerPrefix = "wgk-"

func renderInstallConfig(name string, settings installSettings, privateKey, ipv4Uplink, ipv6Uplink string, port int) []byte {
	var config strings.Builder
	fmt.Fprintf(&config, "[Interface]\nAddress = %s, %s\nListenPort = %d\nPrivateKey = %s\n",
		settings.IPv4, settings.IPv6, port, privateKey)
	writeInstallFirewallHooks(&config, name, "iptables", ipv4Uplink, settings.IPv4.Masked(), port)
	writeInstallFirewallHooks(&config, name, "ip6tables", ipv6Uplink, settings.IPv6.Masked(), port)
	return []byte(config.String())
}

func writeInstallFirewallHooks(config *strings.Builder, name, executable, uplink string, subnet netip.Prefix, port int) {
	owner := installFirewallOwnerPrefix + name
	filterRules := []string{
		fmt.Sprintf("INPUT -p udp --dport %d -j ACCEPT", port),
		fmt.Sprintf("FORWARD -i %s -o %s -s %s -j ACCEPT", name, uplink, subnet),
		fmt.Sprintf("FORWARD -i %s -o %s -d %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", uplink, name, subnet),
	}
	for _, rule := range filterRules {
		writeInstallFirewallRule(config, executable, "-I", rule, owner)
	}
	natRule := fmt.Sprintf("POSTROUTING -o %s -s %s -j MASQUERADE", uplink, subnet)
	writeInstallFirewallRule(config, executable+" -t nat", "-A", natRule, owner)
}

func writeInstallFirewallRule(config *strings.Builder, command, addAction, rule, owner string) {
	rule += " -m comment --comment " + owner
	check := command + " -C " + rule
	fmt.Fprintf(config, "PostUp = %s || %s %s %s\n", check, command, addAction, rule)
	fmt.Fprintf(config, "PostDown = if %s; then %s -D %s; fi\n", check, command, rule)
}
