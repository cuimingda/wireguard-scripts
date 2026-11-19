package main

import "testing"

func TestWireGuardPath(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("PATH", t.TempDir())
	args := []string{"service", "config", "path"}

	checkStatus(t, args, "/etc/wireguard/wg0.conf\n", "", 0)

	checkConfigCommand(t, "", "", "set", "interface", "wg-yaml")
	checkStatus(t, args, "/etc/wireguard/wg-yaml.conf\n", "", 0)
	checkConfigCommand(t, "", "", "unset", "interface")
	checkStatus(t, args, "/etc/wireguard/wg0.conf\n", "", 0)

	writeTestConfig(t, xdg, "interface: 'wg*'\n")
	checkStatus(t, args, "", "invalid interface name", 2)
	checkStatus(t, append(args, "unexpected"), "", "unexpected", 2)
}
