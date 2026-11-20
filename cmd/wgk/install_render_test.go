package main

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"
)

func TestRenderInstallConfig(t *testing.T) {
	settings := installSettings{
		IPv4: netip.MustParsePrefix("10.66.66.1/24"),
		IPv6: netip.MustParsePrefix("fd42:42:42::1/64"),
		Port: 51820,
	}
	data := renderInstallConfig(settings, "test-private-key", "eth0", "eth1")
	config := "\n" + string(data)
	assertLine := func(line string) {
		t.Helper()
		if !strings.Contains(config, "\n"+line+"\n") {
			t.Errorf("configuration is missing %q", line)
		}
	}
	for _, line := range []string{
		"[Interface]",
		"Address = 10.66.66.1/24, fd42:42:42::1/64",
		"ListenPort = 51820",
		"PrivateKey = test-private-key",
	} {
		assertLine(line)
	}
	for _, rule := range []struct{ command, action, args string }{
		{"iptables", "-I", "INPUT -p udp --dport 51820 -j ACCEPT"},
		{"iptables", "-I", "FORWARD -i wg0 -o eth0 -s 10.66.66.0/24 -j ACCEPT"},
		{"iptables", "-I", "FORWARD -i eth0 -o wg0 -d 10.66.66.0/24 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT"},
		{"iptables -t nat", "-A", "POSTROUTING -o eth0 -s 10.66.66.0/24 -j MASQUERADE"},
		{"ip6tables", "-I", "INPUT -p udp --dport 51820 -j ACCEPT"},
		{"ip6tables", "-I", "FORWARD -i wg0 -o eth1 -s fd42:42:42::/64 -j ACCEPT"},
		{"ip6tables", "-I", "FORWARD -i eth1 -o wg0 -d fd42:42:42::/64 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT"},
		{"ip6tables -t nat", "-A", "POSTROUTING -o eth1 -s fd42:42:42::/64 -j MASQUERADE"},
	} {
		args := " " + rule.args + " -m comment --comment wgk-wg0"
		check := rule.command + " -C" + args
		assertLine("PostUp = " + check + " || " + rule.command + " " + rule.action + args)
		assertLine("PostDown = if " + check + "; then " + rule.command + " -D" + args + "; fi")
	}

	change, err := planListenPortChange(data, "45555")
	if err != nil {
		t.Fatal(err)
	}
	if len(change.lines) != 5 || bytes.Contains(change.updated, []byte("51820")) {
		t.Errorf("port change missed generated hooks: %+v", change.lines)
	}
}
