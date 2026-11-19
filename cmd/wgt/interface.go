package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultInterface = "wg0"

var interfaceName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

func resolveInterface() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(configHome) {
		home := os.Getenv("HOME")
		if home == "" {
			return defaultInterface, nil
		}
		configHome = filepath.Join(home, ".config")
	}
	path := filepath.Join(configHome, "wireguard-scripts", "interface")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return defaultInterface, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultInterface, nil
	}
	name := strings.TrimSuffix(string(data), "\n")
	if name == "" {
		return defaultInterface, nil
	}
	if !interfaceName.MatchString(name) || name == "." || name == ".." {
		return "", fmt.Errorf("%s: invalid interface name", path)
	}
	return name, nil
}
