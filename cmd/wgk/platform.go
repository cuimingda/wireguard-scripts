package main

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

func checkUbuntu(goos string, readFile func(string) ([]byte, error)) error {
	if goos != "linux" {
		return fmt.Errorf("Ubuntu is required (detected %s)", goos)
	}

	path := "/etc/os-release"
	data, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		path = "/usr/lib/os-release"
		data, err = readFile(path)
	}
	if err != nil {
		return fmt.Errorf("Ubuntu is required: could not read OS release information: %w", err)
	}

	var id string
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "ID="); ok {
			id = value
		}
	}
	switch id {
	case "ubuntu", `"ubuntu"`, "'ubuntu'":
		return nil
	default:
		return fmt.Errorf("Ubuntu is required (expected ID=ubuntu in %s)", path)
	}
}
