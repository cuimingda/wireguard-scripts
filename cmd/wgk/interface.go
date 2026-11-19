package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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

func validInterfaceName(name string) bool {
	return interfaceName.MatchString(name) && name != "." && name != ".."
}

func legacyInterface(home string) (string, error) {
	if home == "" {
		return "", nil
	}
	path := filepath.Join(home, "wireguard-scripts", "interface")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}
	name := strings.TrimSuffix(string(data), "\n")
	if name != "" && !validInterfaceName(name) {
		return "", fmt.Errorf("%s: invalid interface name", path)
	}
	return name, nil
}
