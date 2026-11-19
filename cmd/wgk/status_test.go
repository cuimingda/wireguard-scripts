package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		properties string
		queryExit  string
		wantOutput string
		wantError  string
		wantExit   int
	}{
		{name: "active", properties: "LoadState=loaded\nActiveState=active\n", wantOutput: "active\n"},
		{name: "inactive", properties: "ActiveState=inactive\nLoadState=loaded\n", wantOutput: "inactive\n", wantExit: 1},
		{name: "missing unit", properties: "LoadState=not-found\nActiveState=inactive\n", wantError: "LoadState=not-found", wantExit: 2},
		{name: "failed unit", properties: "LoadState=loaded\nActiveState=failed\n", wantError: "ActiveState=failed", wantExit: 2},
		{name: "duplicate property", properties: "LoadState=loaded\nLoadState=loaded\nActiveState=active\n", wantError: "unexpected systemctl state", wantExit: 2},
		{name: "malformed property", properties: "LoadState=loaded\nActiveState= active\n", wantError: "unexpected systemctl state", wantExit: 2},
		{name: "missing property", properties: "LoadState=loaded\n", wantError: "missing systemctl state", wantExit: 2},
		{name: "query failure", queryExit: "3", wantError: "exit status 3", wantExit: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", "")
			calls := stubSystemctl(t, tc.properties, tc.queryExit)
			checkStatus(t, []string{"status"}, tc.wantOutput, tc.wantError, tc.wantExit)
			checkQuery(t, calls, "wg0")
		})
	}
}

func TestStatusUsesConfiguredInterface(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	homeConfig := filepath.Join(os.Getenv("HOME"), ".config", "wireguard-scripts")
	xdgConfig := filepath.Join(xdg, "wireguard-scripts")
	writeTestInterface(t, homeConfig, "wg1\n")
	writeTestInterface(t, xdgConfig, "wg-test.2")
	emptyXDG := t.TempDir()
	writeTestInterface(t, filepath.Join(emptyXDG, "wireguard-scripts"), "\n")
	directoryXDG := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directoryXDG, "wireguard-scripts", "interface"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		xdg        string
		wantTarget string
	}{
		{name: "absolute XDG home", xdg: xdg, wantTarget: "wg-test.2"},
		{name: "relative XDG home", xdg: "relative", wantTarget: "wg1"},
		{name: "missing override in absolute XDG home", xdg: t.TempDir(), wantTarget: "wg0"},
		{name: "empty override", xdg: emptyXDG, wantTarget: "wg0"},
		{name: "nonregular override", xdg: directoryXDG, wantTarget: "wg0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tc.xdg)
			calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
			checkStatus(t, []string{"status"}, "active\n", "", 0)
			checkQuery(t, calls, tc.wantTarget)
		})
	}
}

func TestStatusRejectsInvalidInterfaceAndArguments(t *testing.T) {
	for _, tc := range []struct {
		name           string
		interfaceValue string
		args           []string
		wantError      string
	}{
		{name: "multiline", interfaceValue: "wg0\nwg1\n", args: []string{"status"}, wantError: "invalid interface name"},
		{name: "nul", interfaceValue: "wg0\x00", args: []string{"status"}, wantError: "invalid interface name"},
		{name: "parent component", interfaceValue: "..\n", args: []string{"status"}, wantError: "invalid interface name"},
		{name: "extra argument", interfaceValue: "wg0", args: []string{"status", "wg1"}, wantError: "wg1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xdg := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", xdg)
			writeTestInterface(t, filepath.Join(xdg, "wireguard-scripts"), tc.interfaceValue)
			calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
			checkStatus(t, tc.args, "", tc.wantError, 2)
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Fatalf("expected no systemctl invocation, log lookup returned %v", err)
			}
		})
	}
}

func stubSystemctl(t *testing.T, properties, exitStatus string) string {
	t.Helper()
	bin := t.TempDir()
	calls := filepath.Join(bin, "systemctl.args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$WGK_TEST_CALLS"
printf '%s' "$WGK_TEST_PROPERTIES"
exit "${WGK_TEST_EXIT:-0}"
`
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("WGK_TEST_CALLS", calls)
	t.Setenv("WGK_TEST_PROPERTIES", properties)
	t.Setenv("WGK_TEST_EXIT", exitStatus)
	return calls
}

func checkQuery(t *testing.T, calls, name string) {
	t.Helper()
	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	want := "show\n--property=LoadState\n--property=ActiveState\n--\nwg-quick@" + name + ".service\n"
	if string(got) != want {
		t.Errorf("systemctl arguments = %q, want %q", got, want)
	}
}

func checkStatus(t *testing.T, args []string, wantOutput, wantError string, wantExit int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if got := run(context.Background(), args, &stdout, &stderr); got != wantExit {
		t.Errorf("exit code = %d, want %d", got, wantExit)
	}
	if stdout.String() != wantOutput {
		t.Errorf("stdout = %q, want %q", stdout.String(), wantOutput)
	}
	if got := stderr.String(); (wantError == "" && got != "") || !strings.Contains(got, wantError) {
		t.Errorf("stderr = %q, want %q", got, wantError)
	}
}

func writeTestInterface(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "interface"), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
}
