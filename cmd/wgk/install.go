package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
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
}

type installState struct {
	Interface  string          `yaml:"interface"`
	Endpoint   string          `yaml:"endpoint"`
	IPv4Uplink string          `yaml:"ipv4_uplink"`
	IPv6Uplink string          `yaml:"ipv6_uplink"`
	Complete   bool            `yaml:"complete"`
	Port       int             `yaml:"port,omitempty"`
	Attempt    *installAttempt `yaml:"attempt,omitempty"`
}

type installPlan struct {
	state    installState
	config   []byte
	firewall []installFirewallRule
}

type installUnit struct {
	LoadState     string
	ActiveState   string
	UnitFileState string
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
		return provisionWireGuard(cmd, systemInstallPaths, resolveInterface, generateInstallPort)
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
	}, generateInstallPort)
}

func provisionWireGuard(cmd *cobra.Command, paths installPaths, resolveName func() (string, error), generatePort func(*cobra.Command) (int, error)) error {
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
	name, err := resolveName()
	if err != nil {
		return err
	}
	if !validInterfaceName(name) {
		return errors.New("invalid interface name in configuration")
	}
	if err := cleanInstallStagingFiles(paths); err != nil {
		return err
	}
	var oldPort int
	if pending {
		oldPort, err = cleanupInstallAttempt(cmd, paths, state)
		if err != nil {
			return fmt.Errorf("clean up the previous WireGuard installation attempt: %w", err)
		}
	}
	plan, err := planWireGuardInstall(cmd, paths, name, oldPort, generatePort)
	if err != nil {
		return err
	}
	// Persist ownership before anything from this attempt can become visible.
	if err := writeInstallState(statePath, plan.state, pending); err != nil {
		return err
	}
	if err := applyWireGuardInstall(cmd, paths, plan); err != nil {
		return err
	}
	state = plan.state
	state.Complete = true
	state.Attempt = nil
	if err := writeInstallState(statePath, state, true); err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Configured %s (endpoint %s:%d).\n", state.Interface, state.Endpoint, state.Port)
	return err
}

func planWireGuardInstall(cmd *cobra.Command, paths installPaths, name string, oldPort int, generatePort func(*cobra.Command) (int, error)) (installPlan, error) {
	settings, err := loadInstallSettings()
	if err != nil {
		return installPlan{}, err
	}
	state, err := preflightInstallHost(cmd, paths, name, settings)
	if err != nil {
		return installPlan{}, err
	}
	forwarding, err := planInstallForwarding(paths, state)
	if err != nil {
		return installPlan{}, err
	}
	if err := installWireGuardTools(cmd); err != nil {
		return installPlan{}, err
	}
	unit, err := readInstallUnit(cmd, name)
	if err != nil {
		return installPlan{}, err
	}
	if unit.LoadState != "loaded" || unit.ActiveState != "inactive" || unit.UnitFileState != "disabled" {
		return installPlan{}, fmt.Errorf("expected an inactive, disabled %s, found LoadState=%s ActiveState=%s UnitFileState=%s",
			wireGuardServiceUnit(name), unit.LoadState, unit.ActiveState, unit.UnitFileState)
	}
	state.Port, err = chooseInstallPort(cmd, oldPort, generatePort)
	if err != nil {
		return installPlan{}, err
	}
	firewall := installFirewallRules(name, settings, state.IPv4Uplink, state.IPv6Uplink, state.Port)
	if err := preflightInstallFirewall(cmd, firewall); err != nil {
		return installPlan{}, err
	}
	output, err := installCommandOutput(cmd, "wg", "genkey")
	if err != nil {
		return installPlan{}, fmt.Errorf("generate WireGuard private key: %w", err)
	}
	key := strings.TrimSpace(string(output))
	if !validInstallKey(key) {
		return installPlan{}, errors.New("wg genkey did not return a valid WireGuard key")
	}
	publicKey, err := installPublicKey(cmd, key)
	if err != nil {
		return installPlan{}, err
	}
	config := append([]byte(installConfigMark), renderInstallConfig(name, settings, key, state.IPv4Uplink, state.IPv6Uplink, state.Port)...)
	state.Attempt = &installAttempt{
		ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256(config)), PublicKey: publicKey,
		IPv4: settings.IPv4.String(), IPv6: settings.IPv6.String(), Forwarding: forwarding,
	}
	return installPlan{state: state, config: config, firewall: firewall}, nil
}

func applyWireGuardInstall(cmd *cobra.Command, paths installPaths, plan installPlan) error {
	state := plan.state
	configFile := filepath.Join(paths.wireGuardDir, state.Interface+".conf")
	if err := writeDurableInstallFile(configFile, plan.config, false); err != nil {
		return err
	}
	forwardingFile := filepath.Join(paths.sysctlDir, installForwardingName)
	if err := writeDurableInstallFile(forwardingFile, renderInstallForwarding(state.Attempt.Forwarding), false); err != nil {
		return err
	}
	if err := runInstallProcess(cmd, "sysctl", "-p", forwardingFile); err != nil {
		return fmt.Errorf("apply WireGuard forwarding settings: %w", err)
	}
	if err := startInstalledService(cmd, state.Interface, state.Port); err != nil {
		return err
	}
	if err := verifyInstallForwarding(paths, state.Attempt.Forwarding); err != nil {
		return err
	}
	if err := verifyInstallFirewall(cmd, plan.firewall); err != nil {
		return err
	}
	current, err := discoverInstallNetwork(cmd, state.Interface)
	if err != nil {
		return fmt.Errorf("verify public network after enabling WireGuard forwarding: %w", err)
	}
	if current.Endpoint != state.Endpoint || current.IPv4Uplink != state.IPv4Uplink || current.IPv6Uplink != state.IPv6Uplink {
		return errors.New("public endpoint or uplink changed after enabling WireGuard forwarding")
	}
	return enableInstalledService(cmd, state.Interface)
}

func checkExistingInstall(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "params" || strings.HasSuffix(name, ".conf") {
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
		}
	}
	if err := decodeConfigYAML(defaultConfigYAML, &defaults); err != nil {
		return settings, fmt.Errorf("read embedded WireGuard install defaults: %w", err)
	}
	settings.IPv4, _ = netip.ParsePrefix(defaults.Install.IPv4)
	settings.IPv6, _ = netip.ParsePrefix(defaults.Install.IPv6)
	if !settings.IPv4.Addr().Is4() || !settings.IPv4.Addr().IsPrivate() || !settings.IPv6.Addr().Is6() || !settings.IPv6.Addr().IsPrivate() {
		return installSettings{}, errors.New("invalid embedded WireGuard install defaults")
	}
	return settings, nil
}

func installWireGuardTools(cmd *cobra.Command) error {
	for _, tool := range installRequiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			if err := runInstallProcess(cmd, "apt-get", "update"); err != nil {
				return fmt.Errorf("update APT package lists: %w", err)
			}
			args := []string{"install", "-y", "--no-install-recommends", "wireguard-tools", "iptables"}
			if _, err := installCommandOutput(cmd, "apt-get", append([]string{"--simulate"}, args...)...); err != nil {
				return fmt.Errorf("check WireGuard runtime package dependencies: %w", err)
			}
			if err := runInstallProcess(cmd, "apt-get", args...); err != nil {
				return fmt.Errorf("install WireGuard runtime packages: %w", err)
			}
			break
		}
	}
	for _, tool := range installRequiredTools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required WireGuard tool %s is unavailable: %w", tool, err)
		}
	}
	return nil
}

func chooseInstallPort(cmd *cobra.Command, oldPort int, generatePort func(*cobra.Command) (int, error)) (int, error) {
	const maxAttempts = 64
	for range maxAttempts {
		port, err := generatePort(cmd)
		if err != nil {
			return 0, err
		}
		if port < minMaintenancePort || port > maxMaintenancePort {
			return 0, fmt.Errorf("invalid initial WireGuard port %d", port)
		}
		if port == oldPort {
			continue
		}
		if err := probeInstallPort(cmd.Context(), port); errors.Is(err, syscall.EADDRINUSE) {
			continue
		} else if err != nil {
			return 0, err
		}
		return port, nil
	}
	return 0, fmt.Errorf("could not select a new UDP port available on IPv4 and IPv6 after %d attempts", maxAttempts)
}

func probeInstallPort(ctx context.Context, port int) error {
	var listen net.ListenConfig
	for _, network := range []string{"udp4", "udp6"} {
		conn, err := listen.ListenPacket(ctx, network, ":"+strconv.Itoa(port))
		if err != nil {
			return fmt.Errorf("preflight %s listen port %d: %w", network, port, err)
		}
		defer conn.Close()
	}
	return nil
}

func validInstallKey(key string) bool {
	decoded, err := base64.StdEncoding.Strict().DecodeString(key)
	return err == nil && len(decoded) == 32 && base64.StdEncoding.EncodeToString(decoded) == key
}

func installPublicKey(cmd *cobra.Command, privateKey string) (string, error) {
	process := exec.CommandContext(cmd.Context(), "wg", "pubkey")
	process.Stdin = strings.NewReader(privateKey + "\n")
	process.Stderr = cmd.ErrOrStderr()
	output, err := process.Output()
	if err != nil {
		return "", fmt.Errorf("derive WireGuard public key: %w", err)
	}
	publicKey := strings.TrimSpace(string(output))
	if !validInstallKey(publicKey) {
		return "", errors.New("wg pubkey did not return a valid WireGuard key")
	}
	return publicKey, nil
}

func readInstallUnit(cmd *cobra.Command, name string) (installUnit, error) {
	unit := wireGuardServiceUnit(name)
	output, err := installCommandOutput(cmd, "systemctl", "show", "--property=LoadState", "--property=ActiveState", "--property=UnitFileState", "--", unit)
	if err != nil {
		return installUnit{}, fmt.Errorf("query %s before installation: %w", unit, err)
	}
	properties := make(map[string]string, 3)
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			properties[key] = value
		}
	}
	if properties["LoadState"] == "" || properties["ActiveState"] == "" {
		return installUnit{}, fmt.Errorf("incomplete systemctl state for %s", unit)
	}
	return installUnit{LoadState: properties["LoadState"], ActiveState: properties["ActiveState"], UnitFileState: properties["UnitFileState"]}, nil
}

func generateInstallPort(cmd *cobra.Command) (int, error) {
	binary, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("find wgk for port generation: %w", err)
	}
	output, err := installCommandOutput(cmd, binary, "port", "generate", "--"+portGenerateInitialFlag)
	if err != nil {
		return 0, fmt.Errorf("run wgk port generate: %w", err)
	}
	port := strings.TrimRight(string(output), "\n")
	if !validMaintenancePort(port) {
		return 0, errors.New("wgk port generate must return a five-digit port (10000-65535)")
	}
	return decimalListenPort([]byte(port)), nil
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
