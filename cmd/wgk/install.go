package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/unix"
)

const (
	installStateName      = "wgk-install.yaml"
	installConfigMark     = "# Managed by wgk install\n"
	installNeedsInterface = exitStatus(3)
)

type installPaths struct {
	wireGuardDir string
	sysctlDir    string
	procSysDir   string
	systemdDir   string
}

type installSettings struct {
	IPv4 netip.Prefix
	IPv6 netip.Prefix
	Port int
}

type installState struct {
	Interface  string `yaml:"interface"`
	Endpoint   string `yaml:"endpoint"`
	IPv4Uplink string `yaml:"ipv4_uplink"`
	IPv6Uplink string `yaml:"ipv6_uplink"`
	Complete   bool   `yaml:"complete"`
}

func newInstallCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "install",
		Short: "Configure and start the WireGuard server once",
		Args:  cobra.NoArgs,
		RunE:  runInstall,
	}
	command.AddCommand(&cobra.Command{
		Use:    "provision <interface>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE:   runPrivilegedInstall,
	})
	return command
}

var systemInstallPaths = installPaths{
	wireGuardDir: wireGuardConfigDir,
	sysctlDir:    "/etc/sysctl.d",
	procSysDir:   "/proc/sys",
	systemdDir:   "/run/systemd/system",
}

func runInstall(cmd *cobra.Command, _ []string) error {
	if os.Geteuid() == 0 {
		return provisionWireGuard(cmd, systemInstallPaths, resolveInterface)
	}
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find wgk for privileged installation: %w", err)
	}
	// Sudo can change HOME and XDG_CONFIG_HOME. Pass only the resolved name.
	// Defer reporting configuration errors until root checks for an existing installation.
	name, configErr := resolveInterface()
	err = preserveProcessExit(runPrivileged(cmd, binary, "install", "provision", "--", name))
	if configErr != nil && errors.Is(err, installNeedsInterface) {
		return configErr
	}
	return err
}

func runPrivilegedInstall(cmd *cobra.Command, args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("privileged WireGuard installation requires root")
	}
	return provisionWireGuard(cmd, systemInstallPaths, func() (string, error) {
		if args[0] == "" {
			return "", installNeedsInterface
		}
		return args[0], nil
	})
}

func provisionWireGuard(cmd *cobra.Command, paths installPaths, resolveName func() (string, error)) error {
	if err := os.MkdirAll(paths.wireGuardDir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(paths.wireGuardDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("expected a trusted WireGuard directory at %s", paths.wireGuardDir)
	}
	dir, err := os.Open(paths.wireGuardDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := unix.Flock(int(dir.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("lock WireGuard directory for installation: %w", err)
	}

	statePath := filepath.Join(paths.wireGuardDir, installStateName)
	state, pending, err := readInstallState(statePath)
	if err != nil {
		return err
	}
	if state.Complete {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "WireGuard is already configured (%s).\n", state.Interface)
		return err
	}
	var pendingConfig string
	if pending {
		pendingConfig = state.Interface + ".conf"
	} else {
		state.Interface, err = resolveName()
		if err != nil {
			return err
		}
		if !validInterfaceName(state.Interface) {
			return errors.New("invalid interface name in configuration")
		}
	}
	if err := checkExistingInstall(paths.wireGuardDir, pendingConfig); err != nil {
		return err
	}
	if info, err := os.Stat(paths.systemdDir); err != nil || !info.IsDir() {
		return errors.New("WireGuard installation requires a running systemd")
	}
	if !pending {
		if err := checkUnusedInstallTargets(cmd, paths, state.Interface); err != nil {
			return err
		}
	}

	network, err := discoverInstallNetwork(cmd, state.Interface)
	if err != nil {
		return err
	}
	if pending && network != state {
		return errors.New("public address or uplink changed since the pending WireGuard installation")
	}
	settings, err := loadInstallSettings()
	if err != nil {
		return err
	}
	if err := installWireGuardTools(cmd); err != nil {
		return err
	}
	if !pending {
		if err := writeInstallState(statePath, network, false); err != nil {
			return err
		}
		state = network
	}
	port, err := prepareInstallConfig(cmd, filepath.Join(paths.wireGuardDir, state.Interface+".conf"), state, settings)
	if err != nil {
		return err
	}
	if err := applyInstallForwarding(cmd, paths, state, pending); err != nil {
		return err
	}
	if err := startInstalledService(cmd, state.Interface, port); err != nil {
		return err
	}
	current, err := discoverInstallNetwork(cmd, state.Interface)
	if err != nil {
		return fmt.Errorf("verify public network after enabling WireGuard forwarding: %w", err)
	}
	if current != state {
		return errors.New("public endpoint or uplink changed after enabling WireGuard forwarding")
	}
	if err := enableInstalledService(cmd, state.Interface); err != nil {
		return err
	}
	state.Complete = true
	if err := writeInstallState(statePath, state, true); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Configured %s (endpoint %s:%d).\n", state.Interface, state.Endpoint, port)
	return err
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
	if state.Interface == "" {
		// Installation records written before interface selection always used wg0.
		state.Interface = "wg0"
	}
	ip, _ := netip.ParseAddr(state.Endpoint)
	if !validInterfaceName(state.Interface) || !ip.Is4() || serverIPScope(ip) != "public" ||
		!validInstallUplink(state.IPv4Uplink, state.Interface) || !validInstallUplink(state.IPv6Uplink, state.Interface) {
		return state, false, fmt.Errorf("invalid WireGuard installation state in %s", path)
	}
	return state, true, nil
}

func writeInstallState(path string, state installState, replace bool) error {
	data, err := yaml.Marshal(state)
	if err != nil {
		return err
	}
	if err := writeUserConfig(path, data, replace); err != nil {
		return fmt.Errorf("write WireGuard installation state %s: %w", path, err)
	}
	return nil
}

func checkExistingInstall(dir, pendingConfig string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "params" || (strings.HasSuffix(name, ".conf") && name != pendingConfig) {
			return fmt.Errorf("existing WireGuard configuration at %s", filepath.Join(dir, name))
		}
	}
	return nil
}

func checkUnusedInstallTargets(cmd *cobra.Command, paths installPaths, name string) error {
	forwardingPath := filepath.Join(paths.sysctlDir, installForwardingName)
	if _, err := os.Lstat(forwardingPath); err == nil {
		return fmt.Errorf("existing forwarding configuration at %s", forwardingPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return checkExistingInstallLinks(cmd, name)
}

func loadInstallSettings() (installSettings, error) {
	var settings installSettings
	var defaults struct {
		Install struct {
			IPv4 string
			IPv6 string
			Port int
		}
	}
	if err := decodeConfigYAML(defaultConfigYAML, &defaults); err != nil {
		return settings, fmt.Errorf("read embedded WireGuard install defaults: %w", err)
	}
	settings.IPv4, _ = netip.ParsePrefix(defaults.Install.IPv4)
	settings.IPv6, _ = netip.ParsePrefix(defaults.Install.IPv6)
	settings.Port = defaults.Install.Port
	if !settings.IPv4.Addr().Is4() || !settings.IPv4.Addr().IsPrivate() || !settings.IPv6.Addr().Is6() || !settings.IPv6.Addr().IsPrivate() ||
		!validMaintenancePort(strconv.Itoa(settings.Port)) {
		return installSettings{}, errors.New("invalid embedded WireGuard install defaults")
	}
	return settings, nil
}

func installWireGuardTools(cmd *cobra.Command) error {
	tools := []string{"wg", "wg-quick", "iptables", "ip6tables"}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			if err := runInstallProcess(cmd, "apt-get", "update"); err != nil {
				return fmt.Errorf("update APT package lists: %w", err)
			}
			if err := runInstallProcess(cmd, "apt-get", "install", "-y", "--no-install-recommends", "wireguard-tools", "iptables"); err != nil {
				return fmt.Errorf("install WireGuard runtime packages: %w", err)
			}
			break
		}
	}
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required WireGuard tool %s is unavailable: %w", tool, err)
		}
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

func prepareInstallConfig(cmd *cobra.Command, path string, state installState, settings installSettings) (int, error) {
	data, err := readInstallFile(path)
	if err == nil {
		if !bytes.HasPrefix(data, []byte(installConfigMark)) {
			return 0, fmt.Errorf("existing WireGuard configuration at %s", path)
		}
		return parseListenPort(data)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	output, err := installCommandOutput(cmd, "wg", "genkey")
	if err != nil {
		return 0, fmt.Errorf("generate WireGuard private key: %w", err)
	}
	key := strings.TrimSpace(string(output))
	decoded, err := base64.StdEncoding.Strict().DecodeString(key)
	if err != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != key {
		return 0, errors.New("wg genkey did not return a valid WireGuard key")
	}
	config := append([]byte(installConfigMark), renderInstallConfig(state.Interface, settings, key, state.IPv4Uplink, state.IPv6Uplink)...)
	if err := writeUserConfig(path, config, false); err != nil {
		return 0, fmt.Errorf("create WireGuard server configuration %s: %w", path, err)
	}
	return settings.Port, nil
}

func startInstalledService(cmd *cobra.Command, name string, port int) error {
	unit := wireGuardServiceUnit(name)
	if err := runInstallProcess(cmd, "systemctl", "start", "--", unit); err != nil {
		return fmt.Errorf("start %s: %w", unit, err)
	}
	status, err := serviceStatus(cmd.Context(), unit, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("%s is %s after installation", unit, status)
	}
	output, err := installCommandOutput(cmd, "wg", "show", name, "listen-port")
	if err != nil || strings.TrimSpace(string(output)) != strconv.Itoa(port) {
		return fmt.Errorf("%s is not listening on the configured WireGuard port %d", name, port)
	}
	return nil
}

func enableInstalledService(cmd *cobra.Command, name string) error {
	unit := wireGuardServiceUnit(name)
	if err := runInstallProcess(cmd, "systemctl", "enable", "--", unit); err != nil {
		return fmt.Errorf("enable %s: %w", unit, err)
	}
	output, err := installCommandOutput(cmd, "systemctl", "is-enabled", "--", unit)
	if err != nil || strings.TrimSpace(string(output)) != "enabled" {
		return fmt.Errorf("%s is not enabled for boot", unit)
	}
	return nil
}

func installCommandOutput(cmd *cobra.Command, name string, args ...string) ([]byte, error) {
	process := exec.CommandContext(cmd.Context(), name, args...)
	process.Stderr = cmd.ErrOrStderr()
	return process.Output()
}

func runInstallProcess(cmd *cobra.Command, name string, args ...string) error {
	process := exec.CommandContext(cmd.Context(), name, args...)
	process.Stdout = cmd.OutOrStdout()
	process.Stderr = cmd.ErrOrStderr()
	if name == "apt-get" {
		process.Env = append(process.Environ(), "DEBIAN_FRONTEND=noninteractive")
	}
	return process.Run()
}
