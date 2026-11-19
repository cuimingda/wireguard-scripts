package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceCommands(t *testing.T) {
	for _, tc := range []struct {
		command        string
		action         string
		interfaceValue string
		unit           string
		nativeExit     string
		wantExit       int
	}{
		{command: "up", action: "start", unit: "wg-quick@wg0.service", nativeExit: "0"},
		{command: "down", action: "stop", interfaceValue: "wg-test.2\n", unit: "wg-quick@wg-test.2.service", nativeExit: "5", wantExit: 5},
		{command: "up", action: "start", unit: "wg-quick@wg0.service", nativeExit: "signal", wantExit: 143},
	} {
		t.Run(tc.command+"-"+tc.nativeExit, func(t *testing.T) {
			config := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", config)
			if tc.interfaceValue != "" {
				writeTestInterface(t, filepath.Join(config, "wireguard-scripts"), tc.interfaceValue)
			}
			calls, sudoCalls := stubServiceCommands(t)
			t.Setenv("WGK_ACTION_EXIT", tc.nativeExit)

			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), []string{tc.command}, strings.NewReader("input\n"), &stdout, &stderr); code != tc.wantExit {
				t.Errorf("exit code = %d, want %d; stderr = %q", code, tc.wantExit, stderr.String())
			}
			if got := stdout.String(); got != "input\n" {
				t.Errorf("stdout = %q, want forwarded input", got)
			}
			if got := stderr.String(); got != "systemctl diagnostic\n" {
				t.Errorf("stderr = %q, want systemctl diagnostic", got)
			}
			wantArgs := tc.action + "\n--\n" + tc.unit + "\n"
			checkServiceCall(t, calls, wantArgs)
			if os.Geteuid() == 0 {
				if _, err := os.Stat(sudoCalls); !os.IsNotExist(err) {
					t.Errorf("expected no sudo invocation as root, log lookup returned %v", err)
				}
			} else {
				checkServiceCall(t, sudoCalls, "--\nsystemctl\n"+wantArgs)
			}
		})
	}
}

func TestServiceCommandsRejectInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name           string
		args           []string
		interfaceValue string
		wantError      string
	}{
		{name: "extra argument", args: []string{"up", "wg1"}, wantError: "wg1"},
		{name: "invalid interface", args: []string{"down"}, interfaceValue: "wg*", wantError: "invalid interface name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", config)
			writeTestInterface(t, filepath.Join(config, "wireguard-scripts"), tc.interfaceValue)
			calls, sudoCalls := stubServiceCommands(t)
			var stdout, stderr bytes.Buffer
			if got := run(context.Background(), tc.args, nil, &stdout, &stderr); got != 2 {
				t.Errorf("exit code = %d, want 2", got)
			}
			if got := stderr.String(); !strings.Contains(got, tc.wantError) {
				t.Errorf("stderr = %q, want %q", got, tc.wantError)
			}
			for _, log := range []string{calls, sudoCalls} {
				if _, err := os.Stat(log); !os.IsNotExist(err) {
					t.Errorf("expected no invocation, log lookup for %s returned %v", log, err)
				}
			}
		})
	}
}

func TestServiceCommandLaunchFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	if got := run(context.Background(), []string{"up"}, nil, &stdout, &stderr); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
	if got := stderr.String(); !strings.Contains(got, "could not start wg-quick@wg0.service") {
		t.Errorf("stderr = %q, want a launch diagnostic", got)
	}
}

func stubServiceCommands(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	calls := filepath.Join(bin, "systemctl.args")
	sudoCalls := filepath.Join(bin, "sudo.args")
	for name, script := range map[string]string{
		"systemctl": `#!/bin/sh
printf '%s\n' "$@" > "$WGK_ACTION_CALLS"
IFS= read -r input
printf '%s\n' "$input"
printf 'systemctl diagnostic\n' >&2
if [ "$WGK_ACTION_EXIT" = signal ]; then
	kill -TERM "$$"
fi
exit "$WGK_ACTION_EXIT"
`,
		"sudo": `#!/bin/sh
printf '%s\n' "$@" > "$WGK_ACTION_SUDO_CALLS"
shift
exec "$@"
`,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("WGK_ACTION_CALLS", calls)
	t.Setenv("WGK_ACTION_SUDO_CALLS", sudoCalls)
	return calls, sudoCalls
}

func checkServiceCall(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("arguments at %s = %q, want %q", path, got, want)
	}
}
