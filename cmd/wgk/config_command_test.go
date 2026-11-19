package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestConfigCommandLayers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative")
	path := filepath.Join(home, ".config", "wireguard-scripts", "config.yaml")

	checkConfigCommand(t, path+"\n", "", "path")
	defaults := embeddedDefaultList(t)
	checkConfigCommand(t, defaults, "", "list")
	checkConfigCommand(t, defaults, "", "list", "--default")
	checkConfigCommand(t, "", "", "list", "--user")
	checkConfigCommand(t, "", "", "list", "--filter", "missing")
	checkConfigCommand(t, "wg0\n", "", "get", "interface")
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("read-only commands should leave the config directory absent, got %v", err)
	}

	checkConfigCommand(t, "", "", "set", "interface", "wg2")
	checkConfigCommand(t, "", "", "set", "network.retries", "3")
	checkConfigCommand(t, "", "", "set", "network.enabled", "true")
	checkConfigCommand(t, "interface=wg2\nnetwork.enabled=true\nnetwork.retries=3\n", "", "list", "--user")
	checkConfigCommand(t, "interface=wg2\n", "", "list", "--filter", "interface")
	checkConfigCommand(t, defaults, "", "list", "--default")
	checkConfigCommand(t, "interface=wg2\n", "", "list", "--user", "--filter", "interface")
	checkConfigCommand(t, "network.retries=3\n", "", "list", "--filter", "network.retries")
	checkConfigCommand(t, "3\n", "", "get", "network.retries")
	checkConfigCommand(t, "", "interface", "set", "interface", "wg*")
	checkConfigCommand(t, "wg2\n", "", "get", "interface")
	checkConfigCommand(t, "", "", "unset", "interface")
	checkConfigCommand(t, "wg0\n", "", "get", "interface")
	checkConfigCommand(t, "network.enabled=true\nnetwork.retries=3\n", "", "list", "--user")
	checkConfigCommand(t, "", "", "unset", "network.retries")
	checkConfigCommand(t, "network.enabled=true\n", "", "list", "--user")
	checkConfigCommand(t, "", "interface", "unset", "interface")
	checkConfigCommand(t, "", "--default", "list", "--default", "--user")
	checkConfigCommand(t, "", "arg", "path", "unexpected")
	checkConfigCommand(t, "", "--api", "path", "--api")
}

func embeddedDefaultList(t *testing.T) string {
	t.Helper()
	settings, err := readConfigYAML(defaultConfigYAML, "embedded default configuration")
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := writeConfigList(&output, settings); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestConfigCommandRendersComplexValuesOnOneLine(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	peers := []string{
		"wg-first-remote-peer",
		"wg-second-remote-peer",
		"wg-third-remote-peer",
		"wg-fourth-remote-peer",
		"wg-fifth-remote-peer",
	}
	content := "note: \"line one\\nline two\"\npeers: [" + strings.Join(peers, ", ") + "]\n"
	writeTestConfig(t, xdg, content)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"config", "list", "--user"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("list exited %d, stderr = %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "note=") || !strings.HasPrefix(lines[1], "peers=") {
		t.Fatalf("want one line per sorted key, got %q", stdout.String())
	}
	var note string
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(lines[0], "note=")), &note); err != nil || note != "line one\nline two" {
		t.Errorf("note = %q, error = %v", note, err)
	}
	var listedPeers []string
	if err := yaml.Unmarshal([]byte(strings.TrimPrefix(lines[1], "peers=")), &listedPeers); err != nil || !slices.Equal(listedPeers, peers) {
		t.Errorf("peers = %q, want %q, error = %v", listedPeers, peers, err)
	}
	checkConfigCommand(t, strings.TrimPrefix(lines[1], "peers=")+"\n", "", "get", "peers")
}

func TestConfigCommandUsesOnlyCurrentUserYAML(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := filepath.Join(xdg, "wireguard-scripts", "config.yaml")
	obsolete := map[string]string{
		filepath.Join(xdg, "wireguard-scripts", "interface"):                 "wg-legacy\n",
		filepath.Join(xdg, "mingda.dev", "wireguard-toolkit", "config.yaml"): "interface: wg-old-yaml\n",
	}
	for file, contents := range obsolete {
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	checkConfigCommand(t, path+"\n", "", "path")
	checkConfigCommand(t, "wg0\n", "", "get", "interface")
	checkConfigCommand(t, "", "", "list", "--user")
	checkConfigCommand(t, path+"\n", "", "init")
	checkConfigCommand(t, embeddedDefaultList(t), "", "list", "--user")
	checkConfigCommand(t, "", "", "set", "interface", "wg-yaml")
	checkConfigCommand(t, "", "exists", "init")

	calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
	checkStatus(t, []string{"service", "status"}, "active\n", "", 0)
	checkQuery(t, calls, "wg-yaml")
	checkConfigCommand(t, "", "", "unset", "interface")
	checkStatus(t, []string{"service", "status"}, "active\n", "", 0)
	checkQuery(t, calls, "wg0")
}

func TestConfigCommandEdit(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "settings directory")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	path := filepath.Join(xdg, "wireguard-scripts", "config.yaml")
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
	if code != 0 || stdout.String() != path+"\n" || stderr.String() != "editor output\n" {
		t.Fatalf("edit exited %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
	checkConfigCommand(t, "wg-edit\n", "", "get", "interface")
	checkConfigCommand(t, "", "--editor", "edit", "--editor", "")
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
			writeTestConfig(t, xdg, tc.content)
			checkConfigCommand(t, "interface=wg0\n", "", "list", "--default", "--filter", "interface")
			calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
			checkStatus(t, []string{"service", "status"}, "", tc.wantError, 2)
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatalf("expected no systemctl invocation, log lookup returned %v", err)
			}
			calls, sudoCalls := stubServiceCommands(t)
			checkStatus(t, []string{"service", "up"}, "", tc.wantError, 2)
			for _, log := range []string{calls, sudoCalls} {
				if _, err := os.Stat(log); !os.IsNotExist(err) {
					t.Errorf("expected no service invocation, log lookup for %s returned %v", log, err)
				}
			}
		})
	}
}

func checkConfigCommand(t *testing.T, wantOutput, wantError string, args ...string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"config"}, args...), nil, &stdout, &stderr)
	wantExit := 0
	if wantError != "" {
		wantExit = 1
		if !strings.Contains(stderr.String(), wantError) || strings.Count(stderr.String(), "\n") != 1 {
			t.Errorf("config %v stderr = %q, want one diagnostic containing %q", args, stderr.String(), wantError)
		}
	} else if stderr.Len() != 0 {
		t.Errorf("config %v stderr = %q, want empty", args, stderr.String())
	}
	if code != wantExit {
		t.Errorf("config %v exit code = %d, want %d", args, code, wantExit)
	}
	if stdout.String() != wantOutput {
		t.Errorf("config %v stdout = %q, want %q", args, stdout.String(), wantOutput)
	}
}
