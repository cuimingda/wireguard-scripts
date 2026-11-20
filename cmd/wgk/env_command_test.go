package main

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestEnvCommand(t *testing.T) {
	bin := t.TempDir()
	missingGo := filepath.Join(bin, "build-go")
	t.Setenv("PATH", bin)
	t.Setenv("GO", missingGo)
	t.Setenv("XDG_CACHE_HOME", "line one\nline two")
	t.Setenv("GOFLAGS", "-ldflags=private-linker-value")
	t.Setenv("HTTPS_PROXY", "https://user:private-proxy-value@example.invalid/")
	t.Setenv("UNRELATED_TOKEN", "private-environment-value")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeTestConfig(t, xdg, "interface: wg-test\n")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"env"}, nil, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("env exited %d, stderr = %q", code, stderr.String())
	}
	output := stdout.String()
	values := make(map[string]string)
	previousKey := ""
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key <= previousKey {
			t.Fatalf("want a unique, sorted key=value line after %q, got %q", previousKey, line)
		}
		previousKey = key
		values[key] = value
	}
	for key, want := range map[string]string{
		"build.go.arch":                 runtime.GOARCH,
		"build.go.os":                   runtime.GOOS,
		"build.go.version":              runtime.Version(),
		"env.GOFLAGS.set":               "true",
		"env.HTTPS_PROXY.set":           "true",
		"env.XDG_CACHE_HOME":            strconv.Quote("line one\nline two"),
		"runtime.config.path":           filepath.Join(xdg, "wireguard-scripts", "config.yaml"),
		"runtime.wireguard.config_path": "/etc/wireguard/wg-test.conf",
		"runtime.wireguard.interface":   "wg-test",
		"runtime.wireguard.unit":        "wg-quick@wg-test.service",
		"tool.build_go.requested":       missingGo,
		"tool.build_go.path":            "",
		"tool.build_go.version":         "",
		"tool.build_go.env.GOROOT":      "",
		"tool.go.path":                  "",
		"tool.go.version":               "",
		"tool.wg.path":                  "",
	} {
		if got, exists := values[key]; !exists || got != want {
			t.Errorf("%s = %q, present %t, want %q", key, got, exists, want)
		}
	}
	if strings.Contains(output, "private-") {
		t.Error("env exposed a sensitive fixture value")
	}

	stdout.Reset()
	if code := run(context.Background(), []string{"env", "extra"}, nil, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
		t.Errorf("env extra exited %d with stdout %q, want status 2 and empty stdout", code, stdout.String())
	}
}

func TestEnvOSRelease(t *testing.T) {
	data := []byte("# ID=debian\n" +
		"ID='ubuntu'\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n" +
		"PRETTY_NAME=\"Ubuntu \\\"Noble\\\" \\$LTS\\\\\"\nIGNORED=hidden\n")
	values := make(map[string]string)
	addOSReleaseEnvironment(values, func(path string) ([]byte, error) {
		if path == "/etc/os-release" {
			return nil, fs.ErrNotExist
		}
		if path != "/usr/lib/os-release" {
			t.Fatalf("unexpected OS release path %q", path)
		}
		return data, nil
	})
	for key, want := range map[string]string{
		"host.os.id":               "ubuntu",
		"host.os.pretty_name":      "Ubuntu \"Noble\" $LTS\\",
		"host.os.release_file":     "/usr/lib/os-release",
		"host.os.version_codename": "noble",
		"host.os.version_id":       "24.04",
	} {
		if got := values[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if len(values) != 5 {
		t.Errorf("unexpected OS release fields: %v", values)
	}
}
