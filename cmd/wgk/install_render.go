package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const installFirewallOwnerPrefix = "wgk-"

type installFirewallChain struct {
	executable string
	table      string
	name       string
}

type installFirewallRule struct {
	chain     installFirewallChain
	addAction string
	args      []string
}

func (chain installFirewallChain) commandArgs(action string, args ...string) []string {
	var commandArgs []string
	if chain.table != "filter" {
		commandArgs = append(commandArgs, "-t", chain.table)
	}
	return append(append(commandArgs, action, chain.name), args...)
}

func (rule installFirewallRule) command(action string) string {
	return rule.chain.executable + " " + strings.Join(rule.chain.commandArgs(action, rule.args...), " ")
}

func renderInstallConfig(name string, settings installSettings, privateKey, ipv4Uplink, ipv6Uplink string, port int) []byte {
	var config strings.Builder
	fmt.Fprintf(&config, "[Interface]\nAddress = %s, %s\nListenPort = %d\nPrivateKey = %s\n",
		settings.IPv4, settings.IPv6, port, privateKey)
	for _, rule := range installFirewallRules(name, settings, ipv4Uplink, ipv6Uplink, port) {
		check := rule.command("-C")
		fmt.Fprintf(&config, "PostUp = %s || %s\n", check, rule.command(rule.addAction))
		fmt.Fprintf(&config, "PostDown = if %s; then %s; fi\n", check, rule.command("-D"))
	}
	return []byte(config.String())
}

func installFirewallRules(name string, settings installSettings, ipv4Uplink, ipv6Uplink string, port int) []installFirewallRule {
	owner := installFirewallOwnerPrefix + name
	var rules []installFirewallRule
	for _, family := range []struct{ executable, uplink, subnet string }{
		{"iptables", ipv4Uplink, settings.IPv4.Masked().String()},
		{"ip6tables", ipv6Uplink, settings.IPv6.Masked().String()},
	} {
		rulesForFamily := []installFirewallRule{
			{
				chain:     installFirewallChain{family.executable, "filter", "INPUT"},
				addAction: "-I",
				args:      []string{"-p", "udp", "--dport", strconv.Itoa(port), "-j", "ACCEPT"},
			},
			{
				chain:     installFirewallChain{family.executable, "filter", "FORWARD"},
				addAction: "-I",
				args:      []string{"-i", name, "-o", family.uplink, "-s", family.subnet, "-j", "ACCEPT"},
			},
			{
				chain:     installFirewallChain{family.executable, "filter", "FORWARD"},
				addAction: "-I",
				args:      []string{"-i", family.uplink, "-o", name, "-d", family.subnet, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
			},
			{
				chain:     installFirewallChain{family.executable, "nat", "POSTROUTING"},
				addAction: "-A",
				args:      []string{"-o", family.uplink, "-s", family.subnet, "-j", "MASQUERADE"},
			},
		}
		for _, rule := range rulesForFamily {
			rule.args = append(rule.args, "-m", "comment", "--comment", owner)
			rules = append(rules, rule)
		}
	}
	return rules
}

func preflightInstallFirewall(cmd *cobra.Command, rules []installFirewallRule) error {
	present, err := inspectInstallFirewall(cmd, rules)
	if err != nil {
		return err
	}
	if len(present) != 0 {
		return fmt.Errorf("WireGuard firewall rule already exists: %s", present[0].command("-C"))
	}
	for _, executable := range []string{"iptables", "ip6tables"} {
		var payload strings.Builder
		for _, table := range []string{"filter", "nat"} {
			fmt.Fprintln(&payload, "*"+table)
			for _, rule := range rules {
				if rule.chain.executable == executable && rule.chain.table == table {
					fmt.Fprintf(&payload, "-A %s %s\n", rule.chain.name, strings.Join(rule.args, " "))
				}
			}
			fmt.Fprintln(&payload, "COMMIT")
		}
		process := exec.CommandContext(cmd.Context(), executable+"-restore", "--test", "--noflush")
		process.Stdin = strings.NewReader(payload.String())
		if output, err := process.CombinedOutput(); err != nil {
			return fmt.Errorf("preflight %s WireGuard rules: %w%s", executable, err, installFirewallDiagnostic(output))
		}
	}
	return nil
}

func inspectInstallFirewall(cmd *cobra.Command, rules []installFirewallRule) ([]installFirewallRule, error) {
	checkedChains := map[installFirewallChain]bool{}
	var present []installFirewallRule
	for _, rule := range rules {
		if !checkedChains[rule.chain] {
			if output, err := runInstallFirewallCommand(cmd, rule.chain, "-S"); err != nil {
				return nil, fmt.Errorf("inspect %s %s chain %s: %w%s", rule.chain.executable, rule.chain.table, rule.chain.name, err, installFirewallDiagnostic(output))
			}
			checkedChains[rule.chain] = true
		}
		output, err := runInstallFirewallCommand(cmd, rule.chain, "-C", rule.args...)
		if err == nil {
			present = append(present, rule)
		} else if !missingInstallFirewallRule(output, err) {
			return nil, fmt.Errorf("check firewall rule %s: %w%s", rule.command("-C"), err, installFirewallDiagnostic(output))
		}
	}
	return present, nil
}

func verifyInstallFirewall(cmd *cobra.Command, rules []installFirewallRule) error {
	for _, rule := range rules {
		if output, err := runInstallFirewallCommand(cmd, rule.chain, "-C", rule.args...); err != nil {
			return fmt.Errorf("verify WireGuard firewall rule %s: %w%s", rule.command("-C"), err, installFirewallDiagnostic(output))
		}
	}
	return nil
}

func cleanupInstallFirewall(cmd *cobra.Command, rules []installFirewallRule) error {
	present, err := inspectInstallFirewall(cmd, rules)
	if err != nil {
		return err
	}
	for _, rule := range present {
		output, err := runInstallFirewallCommand(cmd, rule.chain, "-D", rule.args...)
		if err != nil && !missingInstallFirewallRule(output, err) {
			return fmt.Errorf("remove WireGuard firewall rule %s: %w%s", rule.command("-D"), err, installFirewallDiagnostic(output))
		}
	}
	return nil
}

func checkInstallFirewallOwnerAbsent(cmd *cobra.Command, name string) error {
	owner := installFirewallOwnerPrefix + name
	for _, executable := range []string{"iptables", "ip6tables"} {
		for _, chain := range []installFirewallChain{
			{executable, "filter", "INPUT"},
			{executable, "filter", "FORWARD"},
			{executable, "nat", "POSTROUTING"},
		} {
			output, err := runInstallFirewallCommand(cmd, chain, "-S")
			if err != nil {
				return fmt.Errorf("inspect %s %s chain %s: %w%s", executable, chain.table, chain.name, err, installFirewallDiagnostic(output))
			}
			for line := range strings.SplitSeq(string(output), "\n") {
				line += " "
				if strings.Contains(line, " --comment "+owner+" ") || strings.Contains(line, " --comment \""+owner+"\" ") {
					return fmt.Errorf("cannot identify earlier WireGuard firewall rules in %s %s chain %s", executable, chain.table, chain.name)
				}
			}
		}
	}
	return nil
}

func runInstallFirewallCommand(cmd *cobra.Command, chain installFirewallChain, action string, args ...string) ([]byte, error) {
	process := exec.CommandContext(cmd.Context(), chain.executable, chain.commandArgs(action, args...)...)
	process.Env = append(process.Environ(), "LC_ALL=C")
	return process.CombinedOutput()
}

func missingInstallFirewallRule(output []byte, err error) bool {
	// Other failures, including insufficient privileges, must stop cleanup.
	return errors.Is(preserveProcessExit(err), exitStatus(1)) && strings.Contains(string(output), "Bad rule (does a matching rule exist in that chain?)")
}

func installFirewallDiagnostic(output []byte) string {
	if diagnostic := strings.TrimSpace(string(output)); diagnostic != "" {
		return ": " + diagnostic
	}
	return ""
}
