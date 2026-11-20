package main

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRenderInstallConfig(t *testing.T) {
	settings := installSettings{
		IPv4: netip.MustParsePrefix("10.66.66.1/24"),
		IPv6: netip.MustParsePrefix("fd42:42:42::1/64"),
	}
	data := renderInstallConfig("wg-2.alt", settings, "test-private-key", "eth0", "eth1", 52731)
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
		"ListenPort = 52731",
		"PrivateKey = test-private-key",
	} {
		assertLine(line)
	}
	for _, rule := range []struct{ command, action, args string }{
		{"iptables", "-I", "INPUT -p udp --dport 52731 -j ACCEPT"},
		{"iptables", "-I", "FORWARD -i wg-2.alt -o eth0 -s 10.66.66.0/24 -j ACCEPT"},
		{"iptables", "-I", "FORWARD -i eth0 -o wg-2.alt -d 10.66.66.0/24 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT"},
		{"iptables -t nat", "-A", "POSTROUTING -o eth0 -s 10.66.66.0/24 -j MASQUERADE"},
		{"ip6tables", "-I", "INPUT -p udp --dport 52731 -j ACCEPT"},
		{"ip6tables", "-I", "FORWARD -i wg-2.alt -o eth1 -s fd42:42:42::/64 -j ACCEPT"},
		{"ip6tables", "-I", "FORWARD -i eth1 -o wg-2.alt -d fd42:42:42::/64 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT"},
		{"ip6tables -t nat", "-A", "POSTROUTING -o eth1 -s fd42:42:42::/64 -j MASQUERADE"},
	} {
		args := " " + rule.args + " -m comment --comment wgk-wg-2.alt"
		check := rule.command + " -C" + args
		assertLine("PostUp = " + check + " || " + rule.command + " " + rule.action + args)
		assertLine("PostDown = if " + check + "; then " + rule.command + " -D" + args + "; fi")
	}

	change, err := planListenPortChange(data, "45555")
	if err != nil {
		t.Fatal(err)
	}
	if len(change.lines) != 5 || bytes.Contains(change.updated, []byte("52731")) {
		t.Errorf("port change missed generated hooks: %+v", change.lines)
	}
}

func TestInstallFirewallCleanupChecksEveryRuleBeforeDeleting(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls")
	t.Setenv("PATH", bin)
	t.Setenv("WGK_TEST_FIREWALL_LOG", logPath)
	t.Setenv("WGK_TEST_FIREWALL_PROBE_ERROR", "yes")
	const tool = `#!/bin/sh
tool=${0##*/}
printf '%s %s\n' "$tool" "$*" >> "$WGK_TEST_FIREWALL_LOG"
case "$tool $*" in
    *' -S '*) exit 0 ;;
    'iptables -C INPUT '*) exit 0 ;;
    'ip6tables -t nat -C POSTROUTING '*)
        if [ "$WGK_TEST_FIREWALL_PROBE_ERROR" = yes ]; then
            echo 'Permission denied' >&2
            exit 1
        fi ;;
    'iptables -D INPUT '*) exit 0 ;;
esac
echo 'Bad rule (does a matching rule exist in that chain?).' >&2
exit 1
`
	for _, name := range []string{"iptables", "ip6tables"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(tool), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	settings := installSettings{IPv4: netip.MustParsePrefix("10.66.66.1/24"), IPv6: netip.MustParsePrefix("fd42:42:42::1/64")}
	rules := installFirewallRules("wg0", settings, "eth0", "eth0", 52731)
	if err := cleanupInstallFirewall(cmd, rules); err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("cleanup error = %v, want a rule inspection error", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(log, []byte(" -D ")) {
		t.Fatalf("cleanup changed rules before completing inspection:\n%s", log)
	}
	t.Setenv("WGK_TEST_FIREWALL_PROBE_ERROR", "no")
	if err := cleanupInstallFirewall(cmd, rules); err != nil {
		t.Fatal(err)
	}
	log, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(log, []byte(" -D ")) != 1 || !bytes.Contains(log, []byte("iptables -D INPUT -p udp --dport 52731 -j ACCEPT -m comment --comment wgk-wg0\n")) {
		t.Fatalf("cleanup did not delete exactly the present WireGuard rule:\n%s", log)
	}
}
