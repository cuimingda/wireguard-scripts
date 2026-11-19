package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommandLayers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative")
	path := filepath.Join(home, ".config", "mingda.dev", "wireguard-toolkit", "config.yaml")

	checkConfigCommand(t, map[string]any{"path": path}, "", "path", "--api")
	checkConfigCommand(t, map[string]any{"interface": "wg0"}, "", "list", "--default")
	checkConfigCommand(t, map[string]any{}, "", "list", "--user")
	checkConfigCommand(t, map[string]any{"key": "interface", "value": "wg0"}, "", "get", "interface")
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("read-only commands should leave the config directory absent, got %v", err)
	}

	checkConfigCommand(t, map[string]any{"key": "interface"}, "", "set", "interface", "wg2")
	checkConfigCommand(t, map[string]any{"key": "network.retries"}, "", "set", "network.retries", "3")
	checkConfigCommand(t, map[string]any{"key": "network.enabled"}, "", "set", "network.enabled", "true")
	checkConfigCommand(t, map[string]any{"interface": "wg0"}, "", "list", "--default")
	checkConfigCommand(t, map[string]any{"interface": "wg2"}, "", "list", "--user", "--filter", "interface")
	checkConfigCommand(t, map[string]any{"network": map[string]any{"retries": 3}}, "", "list", "--filter", "network.retries")
	checkConfigCommand(t, nil, "interface", "set", "interface", "wg*")
	checkConfigCommand(t, map[string]any{"key": "interface", "value": "wg2"}, "", "get", "interface")
	checkConfigCommand(t, map[string]any{"key": "interface"}, "", "unset", "interface")
	checkConfigCommand(t, map[string]any{"key": "interface", "value": "wg0"}, "", "get", "interface")
	checkConfigCommand(t, map[string]any{"network": map[string]any{"enabled": true, "retries": 3}}, "", "list", "--user")
	checkConfigCommand(t, map[string]any{"key": "network.retries"}, "", "unset", "network.retries")
	checkConfigCommand(t, map[string]any{"network": map[string]any{"enabled": true}}, "", "list", "--user")
	checkConfigCommand(t, nil, "interface", "unset", "interface")
	checkConfigCommand(t, nil, "--default", "list", "--default", "--user")
	checkConfigCommand(t, nil, "arg", "path", "unexpected")
}

func TestConfigCommandMigratesLegacyInterface(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := filepath.Join(xdg, "mingda.dev", "wireguard-toolkit", "config.yaml")
	legacyDir := filepath.Join(xdg, "wireguard-scripts")
	writeTestInterface(t, legacyDir, "wg-legacy\n")
	checkConfigCommand(t, map[string]any{"path": path}, "", "init")
	checkConfigCommand(t, map[string]any{"interface": "wg-legacy"}, "", "list", "--user")
	checkConfigCommand(t, map[string]any{"key": "interface"}, "", "set", "interface", "wg-yaml")
	checkConfigCommand(t, nil, "exists", "init")

	writeTestInterface(t, legacyDir, "wg*\n")
	calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
	checkStatus(t, []string{"status"}, "active\n", "", 0)
	checkQuery(t, calls, "wg-yaml")
	writeTestInterface(t, legacyDir, "wg-legacy\n")
	checkConfigCommand(t, map[string]any{"key": "interface"}, "", "unset", "interface")
	checkStatus(t, []string{"status"}, "active\n", "", 0)
	checkQuery(t, calls, "wg-legacy")
}

func TestConfigCommandEdit(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "settings directory")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := filepath.Join(xdg, "mingda.dev", "wireguard-toolkit", "config.yaml")
	editor := filepath.Join(t.TempDir(), "mock editor")
	script := `#!/bin/sh
[ "$1" = "--confirm edit" ] && [ "$#" -eq 2 ] || exit 2
IFS= read -r input
[ "$input" = "requested edit" ] || exit 3
printf 'interface: wg-edit\n' > "$2"
printf 'editor output\n'
`
	if err := os.WriteFile(editor, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"config", "edit", "--editor", `"` + editor + `" "--confirm edit"`}
	code := run(context.Background(), args, strings.NewReader("requested edit\n"), &stdout, &stderr)
	assertConfigResult(t, code, stdout.Bytes(), stderr.String(), map[string]any{"path": path}, "")
	if !strings.Contains(stderr.String(), "editor output\n") {
		t.Errorf("stderr = %q, want editor output", stderr.String())
	}
	checkConfigCommand(t, map[string]any{"key": "interface", "value": "wg-edit"}, "", "get", "interface")
}

func TestInvalidYAMLConfigStopsServiceCommands(t *testing.T) {
	for _, tc := range []struct {
		name, content, wantError string
	}{
		{name: "malformed YAML", content: "interface: [\n", wantError: "config.yaml"},
		{name: "invalid interface", content: "interface: wg*\n", wantError: "interface"},
		{name: "wrong type", content: "interface: 12\n", wantError: "interface"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			writeTestInterface(t, filepath.Join(xdg, "wireguard-scripts"), "wg-legacy\n")
			configDir := filepath.Join(xdg, "mingda.dev", "wireguard-toolkit")
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			checkConfigCommand(t, map[string]any{"interface": "wg0"}, "", "list", "--default")
			calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
			checkStatus(t, []string{"status"}, "", tc.wantError, 2)
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatalf("expected no systemctl invocation, log lookup returned %v", err)
			}
			calls, sudoCalls := stubServiceCommands(t)
			checkStatus(t, []string{"up"}, "", tc.wantError, 2)
			for _, log := range []string{calls, sudoCalls} {
				if _, err := os.Stat(log); !os.IsNotExist(err) {
					t.Errorf("expected no service invocation, log lookup for %s returned %v", log, err)
				}
			}
		})
	}
}

func checkConfigCommand(t *testing.T, wantData any, wantError string, args ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"config"}, args...), nil, &stdout, &stderr)
	assertConfigResult(t, code, stdout.Bytes(), stderr.String(), wantData, wantError)
}

func assertConfigResult(t *testing.T, code int, stdout []byte, stderr string, wantData any, wantError string) {
	t.Helper()
	var result struct {
		OK       *bool           `json:"ok"`
		Data     json.RawMessage `json:"data"`
		Error    json.RawMessage `json:"error"`
		ExitCode *int            `json:"exit_code"`
	}
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("invalid config result %q: %v (exit %d; stderr %q)", stdout, err, code, stderr)
	}
	if result.OK == nil || result.ExitCode == nil || len(result.Data) == 0 || len(result.Error) == 0 {
		t.Fatalf("missing config result fields in %s", stdout)
	}
	wantExit := 0
	if wantError != "" {
		wantExit = 1
		var message string
		if err := json.Unmarshal(result.Error, &message); err != nil || !strings.Contains(message, wantError) {
			t.Errorf("config error = %s, want %q", result.Error, wantError)
		}
	} else if string(result.Error) != "null" {
		t.Errorf("config error = %s, want null", result.Error)
	}
	if code != wantExit || *result.ExitCode != wantExit || *result.OK != (wantExit == 0) {
		t.Errorf("exit code = %d, want %d (result %s; stderr %q)", code, wantExit, stdout, stderr)
	}
	var got any
	if err := json.Unmarshal(result.Data, &got); err != nil {
		t.Fatal(err)
	}
	gotData, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(wantData)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotData, expected) {
		t.Errorf("config data = %s, want %s", gotData, expected)
	}
}
