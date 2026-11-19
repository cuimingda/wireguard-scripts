package main

import (
	"path/filepath"
	"testing"
)

func TestWireGuardPath(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("PATH", t.TempDir())
	args := []string{"service", "config", "path"}

	checkStatus(t, args, "/etc/wireguard/wg0.conf\n", "", 0)

	legacyDir := filepath.Join(config, "wireguard-scripts")
	writeTestInterface(t, legacyDir, "wg-legacy\n")
	checkStatus(t, args, "/etc/wireguard/wg-legacy.conf\n", "", 0)

	checkConfigCommand(t, "", "", "set", "interface", "wg-yaml")
	checkStatus(t, args, "/etc/wireguard/wg-yaml.conf\n", "", 0)
	checkConfigCommand(t, "", "", "unset", "interface")

	writeTestInterface(t, legacyDir, "wg*\n")
	checkStatus(t, args, "", "invalid interface name", 2)
	checkStatus(t, append(args, "unexpected"), "", "unexpected", 2)
}
