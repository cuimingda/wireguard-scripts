package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

const installStagingPrefix = ".wgk-install-staging-"

// An unfinished attempt records how to identify and remove its resources.
// Secrets remain in the WireGuard configuration alone.
type installAttempt struct {
	ConfigSHA256 string          `yaml:"config_sha256"`
	PublicKey    string          `yaml:"public_key"`
	IPv4         string          `yaml:"ipv4"`
	IPv6         string          `yaml:"ipv6"`
	Forwarding   []installSysctl `yaml:"forwarding"`
}

func readInstallState(path string) (installState, bool, error) {
	var state installState
	data, err := readInstallFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	if err := decodeConfigYAML(data, &state); err != nil {
		return state, false, fmt.Errorf("read WireGuard installation state %s: %w", path, err)
	}
	if state.Interface == "" && state.Attempt == nil {
		// Installation records written before interface selection always used wg0.
		state.Interface = "wg0"
	}
	ip, _ := netip.ParseAddr(state.Endpoint)
	if !validInterfaceName(state.Interface) || !ip.Is4() || serverIPScope(ip) != "public" ||
		!validInstallUplink(state.IPv4Uplink, state.Interface) || !validInstallUplink(state.IPv6Uplink, state.Interface) {
		return state, false, fmt.Errorf("invalid WireGuard installation state in %s", path)
	}
	if state.Port != 0 && !validMaintenancePort(strconv.Itoa(state.Port)) {
		return state, false, fmt.Errorf("invalid port in WireGuard installation state %s", path)
	}
	if !state.Complete && state.Port != 0 && state.Attempt == nil {
		return state, false, fmt.Errorf("missing cleanup details in WireGuard installation state %s", path)
	}
	if state.Attempt != nil {
		if state.Port == 0 {
			return state, false, fmt.Errorf("missing port in WireGuard installation state %s", path)
		}
		if err := validateInstallAttempt(state); err != nil {
			return state, false, fmt.Errorf("read WireGuard installation state %s: %w", path, err)
		}
	}
	return state, true, nil
}

func validateInstallAttempt(state installState) error {
	attempt := state.Attempt
	digest, err := hex.DecodeString(attempt.ConfigSHA256)
	if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != attempt.ConfigSHA256 {
		return errors.New("invalid installation configuration hash")
	}
	if !validInstallKey(attempt.PublicKey) {
		return errors.New("invalid installation public key")
	}
	if _, err := installAttemptSettings(attempt.IPv4, attempt.IPv6); err != nil {
		return err
	}
	raKey := "net/ipv6/conf/" + state.IPv6Uplink + "/accept_ra"
	seen := make(map[string]bool, len(attempt.Forwarding))
	for _, entry := range attempt.Forwarding {
		if seen[entry.Key] {
			return errors.New("duplicate installation forwarding setting")
		}
		seen[entry.Key] = true
		switch entry.Key {
		case "net/ipv4/ip_forward", "net/ipv6/conf/all/forwarding":
			if (entry.Before != "0" && entry.Before != "1") || entry.After != "1" {
				return fmt.Errorf("invalid installation forwarding values for %s", entry.Key)
			}
		case raKey:
			if (entry.Before != "1" && entry.Before != "2") || entry.After != "2" {
				return fmt.Errorf("invalid installation forwarding values for %s", entry.Key)
			}
		default:
			return fmt.Errorf("unrecognized installation forwarding setting %q", entry.Key)
		}
	}
	if !seen["net/ipv4/ip_forward"] || !seen["net/ipv6/conf/all/forwarding"] {
		return errors.New("incomplete installation forwarding record")
	}
	return nil
}

func installAttemptSettings(ipv4, ipv6 string) (installSettings, error) {
	v4, err4 := netip.ParsePrefix(ipv4)
	v6, err6 := netip.ParsePrefix(ipv6)
	if err4 != nil || err6 != nil || !v4.Addr().Is4() || !v4.Addr().IsPrivate() ||
		!v6.Addr().Is6() || !v6.Addr().IsPrivate() {
		return installSettings{}, errors.New("invalid tunnel addresses in installation record")
	}
	return installSettings{IPv4: v4, IPv6: v6}, nil
}

func writeInstallState(path string, state installState, replace bool) error {
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	if err := writeDurableInstallFile(path, data, replace); err != nil {
		return fmt.Errorf("write WireGuard installation state %s: %w", path, err)
	}
	return nil
}

func writeDurableInstallFile(path string, data []byte, replace bool) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	stagingPath := filepath.Join(dir.Name(), installStagingPrefix+rand.Text())
	temp, err := os.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if replace {
		if _, err := readInstallFile(path); err != nil {
			return err
		}
		if err := os.Rename(temp.Name(), path); err != nil {
			return err
		}
	} else {
		if err := os.Link(temp.Name(), path); err != nil {
			return err
		}
		if err := os.Remove(temp.Name()); err != nil {
			return err
		}
	}
	return dir.Sync()
}

func cleanInstallStagingFiles(paths installPaths) error {
	for _, dir := range []string{paths.wireGuardDir, paths.sysctlDir} {
		if err := cleanInstallStagingDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}

func cleanInstallStagingDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open installation staging directory %s: %w", path, err)
	}
	dir := os.NewFile(uintptr(fd), path)
	defer dir.Close()
	var directory unix.Stat_t
	if err := unix.Fstat(fd, &directory); err != nil {
		return fmt.Errorf("inspect installation staging directory %s: %w", path, err)
	}
	owner := uint32(os.Geteuid())
	if directory.Uid != owner || directory.Mode&0o022 != 0 {
		return fmt.Errorf("expected a trusted installation staging directory at %s", path)
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read installation staging directory %s: %w", path, err)
	}
	var staged []string
	for _, entry := range entries {
		name := entry.Name()
		suffix, reserved := strings.CutPrefix(name, installStagingPrefix)
		if !reserved {
			continue
		}
		stagingPath := filepath.Join(path, name)
		if len(suffix) < 26 || strings.ContainsFunc(suffix, func(c rune) bool {
			return (c < 'A' || c > 'Z') && (c < '2' || c > '7')
		}) {
			return fmt.Errorf("unrecognized installation staging file %s", stagingPath)
		}
		var file unix.Stat_t
		if err := unix.Fstatat(fd, name, &file, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("inspect installation staging file %s: %w", stagingPath, err)
		}
		if file.Uid != owner || file.Mode&(unix.S_IFMT|0o7177) != unix.S_IFREG {
			return fmt.Errorf("expected a private regular installation staging file at %s", stagingPath)
		}
		staged = append(staged, name)
	}
	if len(staged) == 0 {
		return nil
	}
	for _, name := range staged {
		if err := unix.Unlinkat(fd, name, 0); err != nil {
			return fmt.Errorf("remove installation staging file %s: %w", filepath.Join(path, name), err)
		}
	}
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync installation staging directory %s: %w", path, err)
	}
	return nil
}

func readInstallFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("expected a private regular file at %s", path)
	}
	return os.ReadFile(path)
}

func readOptionalInstallFile(path string) ([]byte, error) {
	data, err := readInstallFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

type installCleanupConfig struct {
	port      int
	publicKey string
	settings  installSettings
}

func inspectInstallCleanupConfig(cmd *cobra.Command, state installState, data []byte) (installCleanupConfig, error) {
	if state.Attempt != nil {
		if data != nil && fmt.Sprintf("%x", sha256.Sum256(data)) != state.Attempt.ConfigSHA256 {
			return installCleanupConfig{}, errors.New("WireGuard configuration changed after the previous installation attempt")
		}
		settings, err := installAttemptSettings(state.Attempt.IPv4, state.Attempt.IPv6)
		return installCleanupConfig{port: state.Port, publicKey: state.Attempt.PublicKey, settings: settings}, err
	}
	if data == nil {
		return installCleanupConfig{}, nil
	}
	// The older installer did not record a hash or public key. Reconstruct
	// the exact installer-generated file before treating it as ours.
	lines := strings.SplitN(string(data), "\n", 6)
	if len(lines) != 6 || lines[0]+"\n" != installConfigMark || lines[1] != "[Interface]" {
		return installCleanupConfig{}, errors.New("cannot verify ownership of the previous WireGuard configuration")
	}
	addresses, hasAddresses := strings.CutPrefix(lines[2], "Address = ")
	ipv4, ipv6, hasIPv6 := strings.Cut(addresses, ", ")
	key, hasKey := strings.CutPrefix(lines[4], "PrivateKey = ")
	port, err := parseListenPort(data)
	if !hasAddresses || !hasIPv6 || !hasKey || !validInstallKey(key) || err != nil || !validMaintenancePort(strconv.Itoa(port)) {
		return installCleanupConfig{}, errors.New("cannot verify ownership of the previous WireGuard configuration")
	}
	settings, err := installAttemptSettings(ipv4, ipv6)
	if err != nil {
		return installCleanupConfig{}, err
	}
	expected := append([]byte(installConfigMark), renderInstallConfig(state.Interface, settings, key, state.IPv4Uplink, state.IPv6Uplink, port)...)
	if !bytes.Equal(data, expected) {
		return installCleanupConfig{}, errors.New("previous WireGuard configuration differs from the installer-generated file")
	}
	publicKey, err := installPublicKey(cmd, key)
	if err != nil {
		return installCleanupConfig{}, fmt.Errorf("derive the previous installation public key: %w", err)
	}
	return installCleanupConfig{port: port, publicKey: publicKey, settings: settings}, nil
}

func verifyInstallCleanupLink(cmd *cobra.Command, name, expectedPublicKey string) (bool, error) {
	links, err := readInstallLinks(cmd)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		if link.Name != name {
			continue
		}
		if link.Info.Kind != "wireguard" || expectedPublicKey == "" {
			return false, fmt.Errorf("cannot verify ownership of existing interface %s", name)
		}
		key, err := installCommandOutput(cmd, "wg", "show", name, "public-key")
		if err != nil {
			return false, fmt.Errorf("read public key of existing interface %s: %w", name, err)
		}
		if strings.TrimSpace(string(key)) != expectedPublicKey {
			return false, fmt.Errorf("existing interface %s has a different public key", name)
		}
		return true, nil
	}
	return false, nil
}

func planInstallCleanupForwarding(paths installPaths, state installState, data []byte) ([]installSysctl, error) {
	if state.Attempt == nil {
		withRA := fmt.Sprintf("net/ipv6/conf/%s/accept_ra = 2\n%s", state.IPv6Uplink, installForwarding)
		if data != nil && string(data) != installForwarding && string(data) != withRA {
			return nil, errors.New("cannot verify ownership of the previous WireGuard forwarding configuration")
		}
		return nil, nil
	}
	if data != nil && !bytes.Equal(data, renderInstallForwarding(state.Attempt.Forwarding)) {
		return nil, errors.New("WireGuard forwarding configuration changed after the previous installation attempt")
	}
	// The file is written before applying settings and removed after restoring
	// them. Its absence means the attempt did not apply them or cleanup finished.
	if data == nil {
		return nil, nil
	}
	var restore []installSysctl
	entries := state.Attempt.Forwarding
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Before == entry.After {
			continue
		}
		value, err := readInstallSysctl(paths, entry.Key)
		if errors.Is(err, os.ErrNotExist) && strings.HasSuffix(entry.Key, "/accept_ra") {
			// The previous uplink may have been removed or renamed.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read forwarding setting %s for cleanup: %w", entry.Key, err)
		}
		if value == entry.After {
			restore = append(restore, entry)
		}
	}
	return restore, nil
}

func cleanupInstallAttempt(cmd *cobra.Command, paths installPaths, state installState) (oldPort int, err error) {
	configPath := filepath.Join(paths.wireGuardDir, state.Interface+".conf")
	data, err := readOptionalInstallFile(configPath)
	if err != nil {
		return 0, err
	}
	config, err := inspectInstallCleanupConfig(cmd, state, data)
	if err != nil {
		return 0, err
	}
	forwardingPath := filepath.Join(paths.sysctlDir, installForwardingName)
	forwarding, err := readOptionalInstallFile(forwardingPath)
	if err != nil {
		return 0, err
	}
	restore, err := planInstallCleanupForwarding(paths, state, forwarding)
	if err != nil {
		return 0, err
	}
	unit := wireGuardServiceUnit(state.Interface)
	status, err := readInstallUnit(cmd, state.Interface)
	if err != nil {
		return 0, err
	}
	if status.LoadState != "loaded" && status.LoadState != "not-found" {
		return 0, fmt.Errorf("cannot clean up %s with load state %s", unit, status.LoadState)
	}
	live, err := verifyInstallCleanupLink(cmd, state.Interface, config.publicKey)
	if err != nil {
		return 0, err
	}
	switch status.UnitFileState {
	case "", "disabled", "enabled":
	default:
		return 0, fmt.Errorf("cannot clean up %s with unit file state %s", unit, status.UnitFileState)
	}
	enabled := status.UnitFileState == "enabled"
	if data == nil && !live && (status.ActiveState != "inactive" || enabled || (state.Attempt == nil && forwarding != nil)) {
		return 0, fmt.Errorf("cannot verify ownership of previous installation resources for %s", state.Interface)
	}
	var rules []installFirewallRule
	if config.port != 0 {
		rules = installFirewallRules(state.Interface, config.settings, state.IPv4Uplink, state.IPv6Uplink, config.port)
		if _, err := inspectInstallFirewall(cmd, rules); err != nil {
			return 0, err
		}
	} else if err := checkInstallFirewallOwnerAbsent(cmd, state.Interface); err != nil {
		return 0, err
	}
	if err := cleanupInstallService(cmd, state.Interface, config.publicKey, status, enabled); err != nil {
		return 0, err
	}
	if len(rules) > 0 {
		if err := cleanupInstallFirewall(cmd, rules); err != nil {
			return 0, err
		}
	}
	for _, entry := range restore {
		value, err := readInstallSysctl(paths, entry.Key)
		if err != nil {
			return 0, fmt.Errorf("recheck forwarding setting %s for cleanup: %w", entry.Key, err)
		}
		if value == entry.After {
			if err := runInstallProcess(cmd, "sysctl", "-w", entry.Key+"="+entry.Before); err != nil {
				return 0, fmt.Errorf("restore forwarding setting %s: %w", entry.Key, err)
			}
		}
	}
	if forwarding != nil && state.Attempt == nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Previous installation did not record original forwarding values. Keeping the current kernel values.")
	}
	if err := removeInstallCleanupFile(forwardingPath, forwarding); err != nil {
		return 0, err
	}
	if err := removeInstallCleanupFile(configPath, data); err != nil {
		return 0, err
	}
	return config.port, nil
}

func cleanupInstallService(cmd *cobra.Command, name, publicKey string, status installUnit, enabled bool) error {
	unit := wireGuardServiceUnit(name)
	var stopErr error
	if status.ActiveState != "inactive" && status.ActiveState != "failed" {
		// A failed ExecStop can leave the unit stopped with work still to
		// clean up. Verify the resulting unit and link before continuing.
		stopErr = runInstallProcess(cmd, "systemctl", "stop", "--", unit)
	}
	current, err := readInstallUnit(cmd, name)
	if err != nil {
		return errors.Join(stopErr, err)
	}
	if current.ActiveState == "failed" {
		if err := runInstallProcess(cmd, "systemctl", "reset-failed", "--", unit); err != nil {
			return fmt.Errorf("reset failed previous installation %s: %w", unit, err)
		}
	} else if current.ActiveState != "inactive" {
		return errors.Join(stopErr, fmt.Errorf("previous installation unit %s is still %s", unit, current.ActiveState))
	}
	live, err := verifyInstallCleanupLink(cmd, name, publicKey)
	if err != nil {
		return err
	}
	if live {
		if err := runInstallProcess(cmd, "ip", "link", "delete", "dev", name); err != nil {
			return fmt.Errorf("remove previous installation interface %s: %w", name, err)
		}
	}
	if enabled {
		if err := runInstallProcess(cmd, "systemctl", "disable", "--", unit); err != nil {
			return fmt.Errorf("disable previous installation %s: %w", unit, err)
		}
	}
	return nil
}

func removeInstallCleanupFile(path string, expected []byte) error {
	current, err := readOptionalInstallFile(path)
	if err != nil || current == nil {
		return err
	}
	if !bytes.Equal(current, expected) || expected == nil {
		return fmt.Errorf("installation file changed during cleanup: %s", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous installation file %s: %w", path, err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
