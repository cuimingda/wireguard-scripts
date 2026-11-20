package main

import (
	"fmt"
	"path/filepath"
	"regexp"
)

const wireGuardConfigDir = "/etc/wireguard"

var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

func resolveInterface() (string, error) {
	settings, err := configSettings(configResolved)
	if err != nil {
		return "", err
	}
	if name, ok := settings["interface"].(string); ok && validInterfaceName(name) {
		return name, nil
	}
	return "", fmt.Errorf("invalid interface name in configuration")
}

func wireGuardConfigPath(name string) string {
	return filepath.Join(wireGuardConfigDir, name+".conf")
}

func validInterfaceName(name string) bool {
	return interfaceName.MatchString(name) && name != "." && name != ".."
}
