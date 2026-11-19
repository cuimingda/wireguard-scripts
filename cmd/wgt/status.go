package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
)

var unitProperty = regexp.MustCompile(`^(LoadState|ActiveState)=([a-z][a-z-]*)$`)

func serviceStatus(ctx context.Context, unit string, stderr io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "show", "--property=LoadState", "--property=ActiveState", "--", unit)
	cmd.Stderr = stderr
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("could not read the state of %s: %w", unit, err)
	}

	properties := make(map[string]string, 2)
	lines := bufio.NewScanner(bytes.NewReader(output))
	for lines.Scan() {
		match := unitProperty.FindStringSubmatch(lines.Text())
		if match == nil || properties[match[1]] != "" {
			return "", fmt.Errorf("unexpected systemctl state for %s", unit)
		}
		properties[match[1]] = match[2]
	}
	if err := lines.Err(); err != nil {
		return "", fmt.Errorf("could not parse the state of %s: %w", unit, err)
	}
	if len(properties) != 2 {
		return "", fmt.Errorf("missing systemctl state for %s", unit)
	}
	loadState, activeState := properties["LoadState"], properties["ActiveState"]
	if loadState != "loaded" || (activeState != "active" && activeState != "inactive") {
		return "", fmt.Errorf("%s has LoadState=%s and ActiveState=%s", unit, loadState, activeState)
	}
	return activeState, nil
}
