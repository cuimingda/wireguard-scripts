package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

const installTestPrivateKey = "AAIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eH2A="

func TestInstallRetriesFailedStartAndStopsAfterSuccess(t *testing.T) {
	paths, calls, cmd, stdout := installTestEnvironment(t)
	configPath := filepath.Join(paths.wireGuardDir, "wg0.conf")
	tool := filepath.Join(os.Getenv("PATH"), "wg-quick")
	if err := os.Rename(tool, tool+".package"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "1")
	if err := provisionWireGuard(cmd, paths); err == nil || !strings.Contains(err.Error(), "wg-quick@wg0.service") {
		t.Fatalf("install returned %v, want a service startup error", err)
	}
	assertInstallState(t, paths, false)
	originalConfig := readInstallTestFile(t, configPath)
	if !bytes.Contains(originalConfig, []byte("PrivateKey = "+installTestPrivateKey)) {
		t.Fatal("the staged configuration does not contain the generated server key")
	}
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected a private server configuration: info = %v, error = %v", info, err)
	}
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "0")
	if err := provisionWireGuard(cmd, paths); err != nil {
		t.Fatalf("retry install: %v", err)
	}
	state := assertInstallState(t, paths, true)
	if bytes.Contains(state, []byte(installTestPrivateKey)) {
		t.Error("installation state duplicates the server key")
	}
	if got := readInstallTestFile(t, configPath); !bytes.Equal(got, originalConfig) {
		t.Fatal("retry changed the generated server configuration")
	}
	forwarding := string(readInstallTestFile(t, filepath.Join(paths.sysctlDir, "90-wgk-wireguard.conf")))
	for _, setting := range []string{"net/ipv6/conf/eth0/accept_ra = 2\n", "net.ipv4.ip_forward = 1\n", "net.ipv6.conf.all.forwarding = 1\n"} {
		if !strings.Contains(forwarding, setting) {
			t.Errorf("forwarding configuration does not contain %q: %q", setting, forwarding)
		}
	}
	before := string(readInstallTestFile(t, calls))
	for call, count := range map[string]int{
		"apt-get update\n": 1,
		"apt-get install -y --no-install-recommends wireguard-tools iptables\n": 1,
		"wg genkey\n": 1,
		"systemctl start -- wg-quick@wg0.service\n":      2,
		"systemctl enable -- wg-quick@wg0.service\n":     1,
		"systemctl is-enabled -- wg-quick@wg0.service\n": 1,
		"wg show wg0 listen-port\n":                      1,
	} {
		if got := strings.Count(before, call); got != count {
			t.Errorf("call %q occurred %d times, want %d; calls: %q", call, got, count, before)
		}
	}
	if strings.Contains(stdout.String(), installTestPrivateKey) {
		t.Fatal("installation output exposed the server key")
	}

	stdout.Reset()
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "1")
	paths.systemdDir = filepath.Join(t.TempDir(), "not-running")
	if err := provisionWireGuard(cmd, paths); err != nil {
		t.Fatalf("repeat successful install: %v", err)
	}
	if !strings.Contains(strings.ToLower(stdout.String()), "already") {
		t.Errorf("repeat install output = %q, expected an already-installed message", stdout.String())
	}
	if got := string(readInstallTestFile(t, calls)); got != before {
		t.Errorf("repeat install executed commands: before = %q, after = %q", before, got)
	}
	if got := assertInstallState(t, paths, true); !bytes.Equal(got, state) {
		t.Error("repeat install changed the completed state")
	}
	if got := readInstallTestFile(t, configPath); !bytes.Equal(got, originalConfig) {
		t.Error("repeat install changed the server configuration")
	}
}

func TestInstallRequiresPublicIPv4BeforeWritingConfiguration(t *testing.T) {
	paths, calls, cmd, _ := installTestEnvironment(t)
	t.Setenv("WGK_TEST_INSTALL_IPV4", "10.0.0.2")
	t.Setenv("WGK_TEST_INSTALL_IPV4_UPLINK", "wgk-test-uplink")
	if err := provisionWireGuard(cmd, paths); err == nil || !strings.Contains(strings.ToLower(err.Error()), "public ipv4") {
		t.Fatalf("install returned %v, want an error about the missing public IPv4", err)
	}
	for _, path := range []string{
		filepath.Join(paths.wireGuardDir, "wg0.conf"),
		filepath.Join(paths.wireGuardDir, "wgk-install.yaml"),
		filepath.Join(paths.sysctlDir, "90-wgk-wireguard.conf"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("failed install changed %s, stat returned %v", path, err)
		}
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(readInstallTestFile(t, calls))), "\n") {
		if !strings.HasPrefix(line, "ip ") {
			t.Errorf("install ran %q with no public IPv4", line)
		}
	}
}

func TestInstallVerifiesTheRunningInterfaceBeforeRecordingSuccess(t *testing.T) {
	paths, _, cmd, _ := installTestEnvironment(t)
	t.Setenv("WGK_TEST_INSTALL_LISTEN_PORT", "51821")
	if err := provisionWireGuard(cmd, paths); err == nil || !strings.Contains(err.Error(), "configured WireGuard port") {
		t.Fatalf("install returned %v, want an error about the interface listen port", err)
	}
	assertInstallState(t, paths, false)
}

func TestInstallProtectsExistingWireGuardConfiguration(t *testing.T) {
	for _, name := range []string{"params", "wg0.conf", "existing.conf"} {
		t.Run(name, func(t *testing.T) {
			paths, calls, cmd, _ := installTestEnvironment(t)
			legacy := filepath.Join(paths.wireGuardDir, name)
			const content = "existing configuration\n"
			if err := os.WriteFile(legacy, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := provisionWireGuard(cmd, paths); err == nil {
				t.Fatalf("install unexpectedly accepted the existing file %s", name)
			}
			if got := readInstallTestFile(t, legacy); string(got) != content {
				t.Errorf("install changed existing file %s", name)
			}
			if _, err := os.Stat(calls); !os.IsNotExist(err) {
				t.Errorf("unexpected tool call with existing file %s; log error = %v", name, err)
			}
		})
	}
}

func TestInstallRejectsEndpointFlag(t *testing.T) {
	checkStatus(t, []string{"install", "--endpoint", "vpn.example.com"}, "", "unknown flag: --endpoint", 2)
}

func installTestEnvironment(t *testing.T) (installPaths, string, *cobra.Command, *bytes.Buffer) {
	t.Helper()
	base := t.TempDir()
	paths := installPaths{
		wireGuardDir: filepath.Join(base, "wireguard"),
		sysctlDir:    filepath.Join(base, "sysctl.d"),
		procSysDir:   filepath.Join(base, "proc", "sys"),
		systemdDir:   filepath.Join(base, "systemd"),
	}
	bin := filepath.Join(base, "bin")
	raPath := filepath.Join(paths.procSysDir, "net", "ipv6", "conf", "eth0", "accept_ra")
	for _, dir := range []string{paths.wireGuardDir, paths.sysctlDir, paths.systemdDir, filepath.Dir(raPath), bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(raPath, []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const tools = `#!/bin/sh
tool=${0##*/}
printf '%s %s\n' "$tool" "$*" >> "$WGK_TEST_INSTALL_CALLS"
case "$tool $*" in
    'apt-get update') ;;
    'apt-get install -y --no-install-recommends wireguard-tools iptables')
        test "$DEBIAN_FRONTEND" = noninteractive && /bin/mv "$PATH/wg-quick.package" "$PATH/wg-quick" ;;
    'ip -j -d link show')
        printf '[]\n' ;;
    'ip -j -4 route get 1.1.1.1')
        printf '[{"dev":"%s","prefsrc":"%s"}]\n' "$WGK_TEST_INSTALL_IPV4_UPLINK" "$WGK_TEST_INSTALL_IPV4" ;;
    'ip -j -6 route get 2606:4700:4700::1111')
        printf '[{"dev":"eth0","prefsrc":"2606:4700:4700::1001"}]\n' ;;
    'wg genkey')
        printf '%s\n' "$WGK_TEST_INSTALL_PRIVATE_KEY" ;;
    'wg show wg0 listen-port')
        printf '%s\n' "$WGK_TEST_INSTALL_LISTEN_PORT" ;;
    'sysctl -p '*)
        test -f "$2" ;;
    'systemctl start -- wg-quick@wg0.service')
        exit "$WGK_TEST_INSTALL_START_CODE" ;;
    'systemctl enable -- wg-quick@wg0.service') ;;
    'systemctl show --property=LoadState --property=ActiveState -- wg-quick@wg0.service')
        printf 'LoadState=loaded\nActiveState=active\n' ;;
    'systemctl is-enabled -- wg-quick@wg0.service')
        printf 'enabled\n' ;;
    *) printf 'unexpected command: %s %s\n' "$tool" "$*" >&2; exit 99 ;;
esac
`
	for _, name := range []string{"apt-get", "ip", "ip6tables", "iptables", "sysctl", "systemctl", "wg", "wg-quick"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(tools), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(base, "calls")
	t.Setenv("PATH", bin)
	t.Setenv("WGK_TEST_INSTALL_CALLS", calls)
	t.Setenv("WGK_TEST_INSTALL_IPV4", "198.71.0.1")
	t.Setenv("WGK_TEST_INSTALL_IPV4_UPLINK", "eth0")
	t.Setenv("WGK_TEST_INSTALL_PRIVATE_KEY", installTestPrivateKey)
	t.Setenv("WGK_TEST_INSTALL_LISTEN_PORT", "51820")
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "0")
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stdout)
	return paths, calls, cmd, stdout
}

func assertInstallState(t *testing.T, paths installPaths, complete bool) []byte {
	t.Helper()
	data := readInstallTestFile(t, filepath.Join(paths.wireGuardDir, "wgk-install.yaml"))
	var state struct {
		Endpoint   string `yaml:"endpoint"`
		IPv4Uplink string `yaml:"ipv4_uplink"`
		IPv6Uplink string `yaml:"ipv6_uplink"`
		Complete   bool   `yaml:"complete"`
	}
	if err := yaml.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Endpoint != "198.71.0.1" || state.IPv4Uplink != "eth0" || state.IPv6Uplink != "eth0" || state.Complete != complete {
		t.Errorf("unexpected install state: %+v, want public IPv4 endpoint, eth0 uplinks and complete=%t", state, complete)
	}
	return data
}

func readInstallTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
