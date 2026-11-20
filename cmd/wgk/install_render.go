package main

import (
	"fmt"
	"net/netip"
	"strings"
)

const installFirewallOwner = "wgk-" + installInterface

func renderInstallConfig(settings installSettings, privateKey, ipv4Uplink, ipv6Uplink string) []byte {
	var config strings.Builder
	fmt.Fprintf(&config, "[Interface]\nAddress = %s, %s\nListenPort = %d\nPrivateKey = %s\n",
		settings.IPv4, settings.IPv6, settings.Port, privateKey)
	writeInstallFirewallHooks(&config, "iptables", ipv4Uplink, settings.IPv4.Masked(), settings.Port)
	writeInstallFirewallHooks(&config, "ip6tables", ipv6Uplink, settings.IPv6.Masked(), settings.Port)
	return []byte(config.String())
}

func writeInstallFirewallHooks(config *strings.Builder, executable, uplink string, subnet netip.Prefix, port int) {
	filterRules := []string{
		fmt.Sprintf("INPUT -p udp --dport %d -j ACCEPT", port),
		fmt.Sprintf("FORWARD -i %s -o %s -s %s -j ACCEPT", installInterface, uplink, subnet),
		fmt.Sprintf("FORWARD -i %s -o %s -d %s -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", uplink, installInterface, subnet),
	}
	for _, rule := range filterRules {
		writeInstallFirewallRule(config, executable, "-I", rule)
	}
	natRule := fmt.Sprintf("POSTROUTING -o %s -s %s -j MASQUERADE", uplink, subnet)
	writeInstallFirewallRule(config, executable+" -t nat", "-A", natRule)
}

func writeInstallFirewallRule(config *strings.Builder, command, addAction, rule string) {
	rule += " -m comment --comment " + installFirewallOwner
	check := command + " -C " + rule
	fmt.Fprintf(config, "PostUp = %s || %s %s %s\n", check, command, addAction, rule)
	fmt.Fprintf(config, "PostDown = if %s; then %s -D %s; fi\n", check, command, rule)
}
