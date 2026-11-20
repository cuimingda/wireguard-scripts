package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

const (
	installTestPrivateKey = "AAIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eH2A="
	installTestPort       = 52731
)

func TestInstallRestartsAfterFailureAndStopsAfterSuccess(t *testing.T) {
	paths, calls, cmd, stdout := installTestEnvironment(t)
	const name = "wg1"
	setInstallTestInterface(t, name)
	t.Setenv("WGK_TEST_INSTALL_INTERFACE", name)
	configPath := filepath.Join(paths.wireGuardDir, name+".conf")
	tool := filepath.Join(os.Getenv("PATH"), "wg-quick")
	if err := os.Rename(tool, tool+".package"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "1")
	generations := 0
	generatePort := func(*cobra.Command) (int, error) {
		generations++
		return installTestPort, nil
	}
	if err := provisionWireGuard(cmd, paths, resolveInterface, generatePort); err == nil || !strings.Contains(err.Error(), "wg-quick@wg1.service") {
		t.Fatalf("install returned %v, want a service startup error", err)
	}
	if generations != 1 {
		t.Fatalf("generated %d ports, want 1", generations)
	}
	assertInstallState(t, paths, name, false)
	originalConfig := readInstallTestFile(t, configPath)
	if !bytes.Contains(originalConfig, []byte("PrivateKey = "+installTestPrivateKey)) {
		t.Fatal("the staged configuration does not contain the generated server key")
	}
	if port, err := parseListenPort(originalConfig); err != nil || port != installTestPort {
		t.Fatalf("staged ListenPort = %d, %v, want %d", port, err, installTestPort)
	}
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected a private server configuration: info = %v, error = %v", info, err)
	}
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "0")
	const retryName = "wg2"
	const retryPort = installTestPort + 1
	setInstallTestInterface(t, retryName)
	t.Setenv("WGK_TEST_INSTALL_INTERFACE", retryName)
	t.Setenv("WGK_TEST_INSTALL_IPV4", "198.71.0.2")
	t.Setenv("WGK_TEST_INSTALL_PRIVATE_KEY", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=")
	t.Setenv("WGK_TEST_INSTALL_LISTEN_PORT", strconv.Itoa(retryPort))
	if err := provisionWireGuard(cmd, paths, resolveInterface, func(*cobra.Command) (int, error) {
		generations++
		if generations == 2 {
			return installTestPort, nil // A fresh attempt must exclude the old port.
		}
		return retryPort, nil
	}); err != nil {
		t.Fatalf("retry install: %v", err)
	}
	if generations != 3 {
		t.Fatalf("generated %d candidates, want 3", generations)
	}
	state := assertInstallState(t, paths, retryName, true)
	if bytes.Contains(state, []byte(installTestPrivateKey)) {
		t.Error("installation state duplicates the server key")
	}
	newConfigPath := filepath.Join(paths.wireGuardDir, retryName+".conf")
	newConfig := readInstallTestFile(t, newConfigPath)
	if port, err := parseListenPort(newConfig); err != nil || port != retryPort || bytes.Contains(newConfig, []byte(installTestPrivateKey)) {
		t.Fatalf("fresh attempt did not use a new port and key, port = %d, error = %v", port, err)
	}
	forwarding := string(readInstallTestFile(t, filepath.Join(paths.sysctlDir, "90-wgk-wireguard.conf")))
	for _, setting := range []string{"net/ipv6/conf/eth0/accept_ra = 2\n", "net/ipv4/ip_forward = 1\n", "net/ipv6/conf/all/forwarding = 1\n"} {
		if !strings.Contains(forwarding, setting) {
			t.Errorf("forwarding configuration does not contain %q: %q", setting, forwarding)
		}
	}
	before := string(readInstallTestFile(t, calls))
	for _, cleanup := range []string{
		"iptables -D INPUT -p udp --dport 52731 -j ACCEPT -m comment --comment wgk-wg1\n",
		"sysctl -w net/ipv4/ip_forward=0\n",
		"sysctl -w net/ipv6/conf/all/forwarding=0\n",
		"sysctl -w net/ipv6/conf/eth0/accept_ra=1\n",
	} {
		if !strings.Contains(before, cleanup) {
			t.Errorf("missing scoped cleanup: %q", cleanup)
		}
	}
	for call, count := range map[string]int{
		"apt-get update\n": 1,
		"apt-get install -y --no-install-recommends wireguard-tools iptables\n": 1,
		"wg genkey\n": 2,
		"systemctl start -- wg-quick@wg1.service\n":      1,
		"systemctl start -- wg-quick@wg2.service\n":      1,
		"systemctl enable -- wg-quick@wg2.service\n":     1,
		"systemctl is-enabled -- wg-quick@wg2.service\n": 1,
		"wg show wg2 listen-port\n":                      1,
	} {
		if got := strings.Count(before, call); got != count {
			t.Errorf("call %q occurred %d times, want %d; calls: %q", call, got, count, before)
		}
	}
	if strings.Contains(stdout.String(), installTestPrivateKey) {
		t.Fatal("installation output exposed the server key")
	}

	stdout.Reset()
	setInstallTestInterface(t, "wg3")
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "1")
	paths.systemdDir = filepath.Join(t.TempDir(), "not-running")
	if err := provisionWireGuard(cmd, paths, unexpectedInstallInterfaceResolution(t), unexpectedInstallPortGeneration(t)); err != nil {
		t.Fatalf("repeat successful install: %v", err)
	}
	if !strings.Contains(strings.ToLower(stdout.String()), "already") {
		t.Errorf("repeat install output = %q, expected an already-installed message", stdout.String())
	}
	if got := string(readInstallTestFile(t, calls)); got != before {
		t.Errorf("repeat install executed commands: before = %q, after = %q", before, got)
	}
	if got := assertInstallState(t, paths, retryName, true); !bytes.Equal(got, state) {
		t.Error("repeat install changed the completed state")
	}
	if got := readInstallTestFile(t, newConfigPath); !bytes.Equal(got, newConfig) {
		t.Error("repeat install changed the server configuration")
	}
	for _, other := range []string{"wg0.conf", "wg1.conf", "wg3.conf"} {
		if _, err := os.Stat(filepath.Join(paths.wireGuardDir, other)); !os.IsNotExist(err) {
			t.Errorf("unexpected install configuration %s, stat returned %v", other, err)
		}
	}
}

func TestInstallReadsLegacyStateWithoutInterface(t *testing.T) {
	const legacy = "endpoint: 198.71.0.1\nipv4_uplink: eth0\nipv6_uplink: eth0\ncomplete: "
	t.Run("pending", func(t *testing.T) {
		paths, _, cmd, _ := installTestEnvironment(t)
		setInstallTestInterface(t, "wg1")
		t.Setenv("WGK_TEST_INSTALL_INTERFACE", "wg1")
		statePath := filepath.Join(paths.wireGuardDir, "wgk-install.yaml")
		if err := os.WriteFile(statePath, []byte(legacy+"false\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(paths.wireGuardDir, "wg0.conf")
		settings, err := loadInstallSettings()
		if err != nil {
			t.Fatal(err)
		}
		config := append([]byte(installConfigMark), renderInstallConfig("wg0", settings, installTestPrivateKey, "eth0", "eth0", 51820)...)
		if err := os.WriteFile(configPath, config, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := provisionWireGuard(cmd, paths, resolveInterface, func(*cobra.Command) (int, error) { return installTestPort, nil }); err != nil {
			t.Fatalf("restart legacy installation: %v", err)
		}
		assertInstallState(t, paths, "wg1", true)
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Errorf("legacy configuration was not removed: %v", err)
		}
	})
	t.Run("complete", func(t *testing.T) {
		paths, calls, cmd, stdout := installTestEnvironment(t)
		setInstallTestInterface(t, "wg1")
		statePath := filepath.Join(paths.wireGuardDir, "wgk-install.yaml")
		data := []byte(legacy + "true\n")
		if err := os.WriteFile(statePath, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := provisionWireGuard(cmd, paths, unexpectedInstallInterfaceResolution(t), unexpectedInstallPortGeneration(t)); err != nil {
			t.Fatalf("check legacy completed installation: %v", err)
		}
		if !strings.Contains(strings.ToLower(stdout.String()), "already") {
			t.Errorf("repeat install output = %q, expected an already-installed message", stdout.String())
		}
		if _, err := os.Stat(calls); !os.IsNotExist(err) {
			t.Errorf("unexpected tool call for completed legacy install; log error = %v", err)
		}
		if got := readInstallTestFile(t, statePath); !bytes.Equal(got, data) {
			t.Error("repeat install changed the legacy completed state")
		}
	})
}

func TestInstallRequiresPublicIPv4BeforeWritingConfiguration(t *testing.T) {
	paths, calls, cmd, _ := installTestEnvironment(t)
	t.Setenv("WGK_TEST_INSTALL_IPV4", "10.0.0.2")
	t.Setenv("WGK_TEST_INSTALL_IPV4_UPLINK", "wgk-test-uplink")
	if err := provisionWireGuard(cmd, paths, resolveInterface, unexpectedInstallPortGeneration(t)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "public ipv4") {
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
		if strings.HasPrefix(line, "apt-get ") || strings.HasPrefix(line, "sysctl -p ") || strings.HasPrefix(line, "systemctl start ") {
			t.Errorf("install ran %q with no public IPv4", line)
		}
	}
}

func TestInstallPreflightStopsBeforeDeployment(t *testing.T) {
	for _, test := range []struct {
		name, env, value, diagnostic string
	}{
		{"public IPv6 missing", "WGK_TEST_INSTALL_IPV6", "fd42::2", "public IPv6"},
		{"overlapping subnet", "WGK_TEST_INSTALL_ROUTES", `[{"dst":"10.66.66.0/24","dev":"eth0"}]`, "overlap"},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths, calls, cmd, _ := installTestEnvironment(t)
			t.Setenv(test.env, test.value)
			if err := provisionWireGuard(cmd, paths, resolveInterface, unexpectedInstallPortGeneration(t)); err == nil || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("preflight error = %v, want %q", err, test.diagnostic)
			}
			for _, path := range []string{
				filepath.Join(paths.wireGuardDir, installStateName), filepath.Join(paths.wireGuardDir, "wg0.conf"),
				filepath.Join(paths.sysctlDir, installForwardingName),
			} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("preflight created %s: %v", path, err)
				}
			}
			if log := string(readInstallTestFile(t, calls)); strings.Contains(log, "apt-get ") || strings.Contains(log, "systemctl start ") || strings.Contains(log, "sysctl -p ") {
				t.Fatalf("preflight attempted deployment: %s", log)
			}
		})
	}
}

func TestInstallPortSkipsAnOccupiedIPv6Port(t *testing.T) {
	occupied, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	occupiedPort := occupied.LocalAddr().(*net.UDPAddr).Port
	if occupiedPort < minMaintenancePort || occupiedPort == installTestPort {
		t.Skipf("test received unsuitable ephemeral port %d", occupiedPort)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var draws int
	port, err := chooseInstallPort(cmd, 0, func(*cobra.Command) (int, error) {
		draws++
		if draws == 1 {
			return occupiedPort, nil
		}
		return installTestPort, nil
	})
	if err != nil || port != installTestPort || draws != 2 {
		t.Fatalf("selected port = %d after %d draws, error = %v", port, draws, err)
	}
}

func TestInstallRejectsInvalidConfiguredInterfaceBeforeRunningTools(t *testing.T) {
	paths, calls, cmd, _ := installTestEnvironment(t)
	configPath, err := userConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("interface: ../wg0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := provisionWireGuard(cmd, paths, resolveInterface, unexpectedInstallPortGeneration(t)); err == nil || !strings.Contains(err.Error(), "invalid interface name") {
		t.Fatalf("install returned %v, want an error about the invalid interface name", err)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Errorf("unexpected tool call with invalid interface name; log error = %v", err)
	}
}

func TestInstallVerifiesTheRunningInterfaceBeforeRecordingSuccess(t *testing.T) {
	paths, _, cmd, _ := installTestEnvironment(t)
	t.Setenv("WGK_TEST_INSTALL_LISTEN_PORT", strconv.Itoa(installTestPort+1))
	generatePort := func(*cobra.Command) (int, error) { return installTestPort, nil }
	if err := provisionWireGuard(cmd, paths, resolveInterface, generatePort); err == nil || !strings.Contains(err.Error(), "configured WireGuard port") {
		t.Fatalf("install returned %v, want an error about the interface listen port", err)
	}
	assertInstallState(t, paths, "wg0", false)
}

func TestInstallPreservesAnEditedConfigurationFromAnIncompleteAttempt(t *testing.T) {
	paths, calls, cmd, _ := installTestEnvironment(t)
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "1")
	if err := provisionWireGuard(cmd, paths, resolveInterface, func(*cobra.Command) (int, error) { return installTestPort, nil }); err == nil {
		t.Fatal("expected the initial installation to fail")
	}
	configPath := filepath.Join(paths.wireGuardDir, "wg0.conf")
	modified := append(readInstallTestFile(t, configPath), []byte("\n# Operator edit\n")...)
	if err := os.WriteFile(configPath, modified, 0o600); err != nil {
		t.Fatal(err)
	}
	before := readInstallTestFile(t, calls)
	if err := provisionWireGuard(cmd, paths, resolveInterface, unexpectedInstallPortGeneration(t)); err == nil || !strings.Contains(err.Error(), "configuration changed") {
		t.Fatalf("retry error = %v, want ownership error", err)
	}
	if !bytes.Equal(readInstallTestFile(t, configPath), modified) || !bytes.Equal(readInstallTestFile(t, calls), before) {
		t.Fatal("retry changed host resources after finding an edited configuration")
	}
}

func TestInstallCleansOnlyRecognizedStagingFiles(t *testing.T) {
	paths, _, _, _ := installTestEnvironment(t)
	staged := filepath.Join(paths.wireGuardDir, installStagingPrefix+strings.Repeat("A", 26))
	foreign := filepath.Join(paths.wireGuardDir, ".other-file")
	link := filepath.Join(paths.wireGuardDir, installStagingPrefix+strings.Repeat("B", 26))
	for _, path := range []string{staged, foreign} {
		if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(foreign, link); err != nil {
		t.Fatal(err)
	}
	if err := cleanInstallStagingFiles(paths); err == nil || !strings.Contains(err.Error(), link) {
		t.Fatalf("staging cleanup error = %v, want error for symbolic link", err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("cleanup modified staging before verifying the directory: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := cleanInstallStagingFiles(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("staging file remains: %v", err)
	}
	if got := readInstallTestFile(t, foreign); string(got) != "data" {
		t.Error("cleanup changed an unrelated file")
	}
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
			if err := provisionWireGuard(cmd, paths, resolveInterface, unexpectedInstallPortGeneration(t)); err == nil {
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
	for _, dir := range []string{paths.wireGuardDir, paths.sysctlDir, paths.systemdDir, bin} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{
		"net/ipv4/ip_forward": "0", "net/ipv6/conf/all/forwarding": "0", "net/ipv6/conf/eth0/accept_ra": "1",
	} {
		path := filepath.Join(paths.procSysDir, key)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const tools = `#!/bin/sh
tool=${0##*/}
printf '%s %s\n' "$tool" "$*" >> "$WGK_TEST_INSTALL_CALLS"
case "$tool $*" in
    'apt-get update'|'apt-get --simulate install -y --no-install-recommends wireguard-tools iptables') ;;
    'apt-get install -y --no-install-recommends wireguard-tools iptables')
        test "$DEBIAN_FRONTEND" = noninteractive && /bin/mv "$PATH/wg-quick.package" "$PATH/wg-quick" ;;
    'ip -j -d link show')
        printf '[]\n' ;;
    'ip -j -4 route show table all'|'ip -j -6 route show table all')
        printf '%s\n' "$WGK_TEST_INSTALL_ROUTES" ;;
    'ip -j -4 route get 1.1.1.1')
        printf '[{"dev":"%s","prefsrc":"%s"}]\n' "$WGK_TEST_INSTALL_IPV4_UPLINK" "$WGK_TEST_INSTALL_IPV4" ;;
    'ip -j -6 route get 2606:4700:4700::1111')
        printf '[{"dev":"eth0","prefsrc":"%s"}]\n' "$WGK_TEST_INSTALL_IPV6" ;;
    'wg genkey')
        printf '%s\n' "$WGK_TEST_INSTALL_PRIVATE_KEY" ;;
    'wg pubkey')
        read -r key && printf '%s\n' "$key" ;;
    "wg show $WGK_TEST_INSTALL_INTERFACE listen-port")
        printf '%s\n' "$WGK_TEST_INSTALL_LISTEN_PORT" ;;
    'sysctl -p '*)
        while read -r key equals value; do
            case "$key" in net/*) printf '%s\n' "$value" > "$WGK_TEST_INSTALL_PROC/$key" || exit 1 ;; esac
        done < "$2" ;;
    'sysctl -w '*)
        printf '%s\n' "${2#*=}" > "$WGK_TEST_INSTALL_PROC/${2%%=*}" ;;
    'systemctl show --property=Version --value')
        printf '255\n' ;;
    'systemctl show --property=LoadState --property=ActiveState --property=UnitFileState -- wg-quick@'*)
        active=inactive
        if test -f "$WGK_TEST_INSTALL_UNIT"; then read -r active < "$WGK_TEST_INSTALL_UNIT"; fi
        printf 'LoadState=loaded\nActiveState=%s\nUnitFileState=disabled\n' "$active" ;;
    'systemctl reset-failed -- wg-quick@'*|'systemctl stop -- wg-quick@'*)
        printf 'inactive\n' > "$WGK_TEST_INSTALL_UNIT" ;;
    "systemctl start -- wg-quick@$WGK_TEST_INSTALL_INTERFACE.service")
        if test "$WGK_TEST_INSTALL_START_CODE" = 0; then
            printf 'active\n' > "$WGK_TEST_INSTALL_UNIT"
            printf 'all\n' > "$WGK_TEST_INSTALL_FIREWALL"
        else
            printf 'failed\n' > "$WGK_TEST_INSTALL_UNIT"
            printf 'partial\n' > "$WGK_TEST_INSTALL_FIREWALL"
            exit "$WGK_TEST_INSTALL_START_CODE"
        fi ;;
    "systemctl enable -- wg-quick@$WGK_TEST_INSTALL_INTERFACE.service") ;;
    "systemctl show --property=LoadState --property=ActiveState -- wg-quick@$WGK_TEST_INSTALL_INTERFACE.service")
        printf 'LoadState=loaded\nActiveState=active\n' ;;
    "systemctl is-enabled -- wg-quick@$WGK_TEST_INSTALL_INTERFACE.service")
        printf 'enabled\n' ;;
    'iptables-restore --test --noflush'|'ip6tables-restore --test --noflush')
        /bin/cat >/dev/null ;;
    'iptables -S '*|'ip6tables -S '*|'iptables -t nat -S '*|'ip6tables -t nat -S '*) ;;
    'iptables -C '*|'ip6tables -C '*|'iptables -t nat -C '*|'ip6tables -t nat -C '*)
        rules=none
        if test -f "$WGK_TEST_INSTALL_FIREWALL"; then read -r rules < "$WGK_TEST_INSTALL_FIREWALL"; fi
        if test "$rules" = all; then exit 0; fi
        case "$rules $tool $*" in 'partial iptables -C INPUT '*) exit 0 ;; esac
        printf '%s\n' 'Bad rule (does a matching rule exist in that chain?).' >&2
        exit 1 ;;
    'iptables -D INPUT '*)
        printf 'none\n' > "$WGK_TEST_INSTALL_FIREWALL" ;;
    *) printf 'unexpected command: %s %s\n' "$tool" "$*" >&2; exit 99 ;;
esac
`
	for _, name := range []string{"apt-get", "ip", "ip6tables", "ip6tables-restore", "iptables", "iptables-restore", "sysctl", "systemctl", "wg", "wg-quick"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(tools), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(base, "calls")
	t.Setenv("PATH", bin)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("WGK_TEST_INSTALL_CALLS", calls)
	t.Setenv("WGK_TEST_INSTALL_PROC", paths.procSysDir)
	t.Setenv("WGK_TEST_INSTALL_UNIT", filepath.Join(base, "unit"))
	t.Setenv("WGK_TEST_INSTALL_FIREWALL", filepath.Join(base, "firewall"))
	t.Setenv("WGK_TEST_INSTALL_INTERFACE", "wg0")
	t.Setenv("WGK_TEST_INSTALL_IPV4", "198.71.0.1")
	t.Setenv("WGK_TEST_INSTALL_IPV6", "2606:4700:4700::1001")
	t.Setenv("WGK_TEST_INSTALL_ROUTES", "[]")
	t.Setenv("WGK_TEST_INSTALL_IPV4_UPLINK", "eth0")
	t.Setenv("WGK_TEST_INSTALL_PRIVATE_KEY", installTestPrivateKey)
	t.Setenv("WGK_TEST_INSTALL_LISTEN_PORT", strconv.Itoa(installTestPort))
	t.Setenv("WGK_TEST_INSTALL_START_CODE", "0")
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stdout)
	return paths, calls, cmd, stdout
}

func setInstallTestInterface(t *testing.T, name string) {
	t.Helper()
	if err := setConfigValue("interface", name); err != nil {
		t.Fatal(err)
	}
}

func unexpectedInstallInterfaceResolution(t *testing.T) func() (string, error) {
	t.Helper()
	return func() (string, error) {
		t.Fatal("install resolved the current interface instead of using recorded state")
		return "", nil
	}
}

func unexpectedInstallPortGeneration(t *testing.T) func(*cobra.Command) (int, error) {
	t.Helper()
	return func(*cobra.Command) (int, error) {
		t.Fatal("install tried to generate a new port")
		return 0, nil
	}
}

func assertInstallState(t *testing.T, paths installPaths, name string, complete bool) []byte {
	t.Helper()
	data := readInstallTestFile(t, filepath.Join(paths.wireGuardDir, "wgk-install.yaml"))
	var state struct {
		Interface  string `yaml:"interface"`
		Endpoint   string `yaml:"endpoint"`
		IPv4Uplink string `yaml:"ipv4_uplink"`
		IPv6Uplink string `yaml:"ipv6_uplink"`
		Complete   bool   `yaml:"complete"`
	}
	if err := yaml.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Interface != name || state.Endpoint != os.Getenv("WGK_TEST_INSTALL_IPV4") || state.IPv4Uplink != "eth0" || state.IPv6Uplink != "eth0" || state.Complete != complete {
		t.Errorf("unexpected install state: %+v, want interface %s, public IPv4 endpoint, eth0 uplinks and complete=%t", state, name, complete)
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
