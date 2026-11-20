package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	minMaintenancePort      = 10000
	maxMaintenancePort      = 65535
	portGetInterfaceFlag    = "interface"
	portGenerateInitialFlag = "initial"
)

func newPortCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "port",
		Short: "Read, change, and check the WireGuard listen port",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	set := &cobra.Command{
		Use:   "set <port>",
		Short: "Change the WireGuard listen port after confirmation",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPortSet,
	}
	set.Flags().BoolP("force", "f", false, "Apply the port change without prompting")
	setInterface := &cobra.Command{
		Use:    "set-interface <interface> <port>",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE:   runPrivilegedPortSet,
	}
	setInterface.Flags().Bool("force", false, "Apply the port change without prompting")
	get := &cobra.Command{
		Use:   "get",
		Short: "Print the configured WireGuard listen port",
		Args:  cobra.NoArgs,
		RunE:  runPortGet,
	}
	get.Flags().String(portGetInterfaceFlag, "", "Use the interface already resolved by the calling command")
	_ = get.Flags().MarkHidden(portGetInterfaceFlag)
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate a random port different from the configured listen port",
		Args:  cobra.NoArgs,
		RunE:  runPortGenerate,
	}
	generate.Flags().Bool(portGenerateInitialFlag, false, "Generate a port before the interface has been configured")
	_ = generate.Flags().MarkHidden(portGenerateInitialFlag)
	command.AddCommand(
		get,
		generate,
		set,
		&cobra.Command{
			Use:    "read <interface>",
			Hidden: true,
			Args:   cobra.ExactArgs(1),
			RunE:   runPrivilegedPortRead,
		},
		setInterface,
		newPortCheckCommand("udp", "List local UDP sockets for the WireGuard port", "ss", func(cmd *cobra.Command, _, port string) error {
			return runPrivileged(cmd, "ss", "-H", "-uln", "sport = :"+port)
		}),
		newPortCheckCommand("iptables", "Check the UDP INPUT rule for the WireGuard port", "iptables", checkIPTablesPort),
	)
	return command
}

func runPortGet(cmd *cobra.Command, _ []string) error {
	if cmd.Flags().Changed(portGetInterfaceFlag) {
		name, err := cmd.Flags().GetString(portGetInterfaceFlag)
		if err != nil {
			return err
		}
		if !validInterfaceName(name) {
			return errors.New("invalid interface name for port lookup")
		}
		return readConfiguredPort(cmd, name, cmd.OutOrStdout())
	}
	name, err := resolveInterface()
	if err != nil {
		return err
	}
	return readConfiguredPort(cmd, name, cmd.OutOrStdout())
}

func readConfiguredPort(cmd *cobra.Command, name string, output io.Writer) error {
	path := wireGuardConfigPath(name)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrPermission) && os.Geteuid() != 0 {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find wgk for privileged port lookup: %w", err)
		}
		process := privilegedCommand(cmd, executable, "port", "read", "--", name)
		process.Stdout = output
		if err := process.Run(); err != nil {
			return preserveProcessExit(fmt.Errorf("read listen port from %s: %w", path, err))
		}
		return nil
	}
	if err != nil {
		return err
	}
	return printListenPort(output, path, data)
}

func runPrivilegedPortRead(cmd *cobra.Command, args []string) error {
	name := args[0]
	if os.Geteuid() != 0 {
		return errors.New("privileged port lookup requires root")
	}
	if !validInterfaceName(name) {
		return errors.New("invalid interface name for privileged port lookup")
	}
	path := wireGuardConfigPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return printListenPort(cmd.OutOrStdout(), path, data)
}

func printListenPort(output io.Writer, path string, data []byte) error {
	port, err := parseListenPort(data)
	if err != nil {
		return fmt.Errorf("read listen port from %s: %w", path, err)
	}
	_, err = fmt.Fprintln(output, port)
	return err
}

func newPortCheckCommand(name, description, executable string, check func(*cobra.Command, string, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			interfaceName, err := resolveInterface()
			if err != nil {
				return err
			}
			port, err := getPortForCheck(cmd, interfaceName)
			if err != nil {
				return err
			}
			if err := check(cmd, interfaceName, port); err != nil {
				return preserveProcessExit(fmt.Errorf("could not run %s: %w", executable, err))
			}
			return nil
		},
	}
}

func checkIPTablesPort(cmd *cobra.Command, name, port string) error {
	check := func(args []string) (*bytes.Buffer, *bytes.Buffer, error) {
		var stdout, stderr bytes.Buffer
		process := privilegedCommand(cmd, "iptables", args...)
		process.Stdout = &stdout
		process.Stderr = &stderr
		err := process.Run()
		return &stdout, &stderr, err
	}

	args := []string{"-C", "INPUT", "-p", "udp", "--dport", port, "-j", "ACCEPT"}
	stdout, stderr, err := check(args)
	if errors.Is(preserveProcessExit(err), exitStatus(1)) {
		// The check must include the owner match to find a rule created by wgk install.
		taggedArgs := append(args, "-m", "comment", "--comment", installFirewallOwnerPrefix+name)
		taggedOut, taggedErr, taggedCheckErr := check(taggedArgs)
		if !errors.Is(preserveProcessExit(taggedCheckErr), exitStatus(1)) {
			stdout, stderr, err = taggedOut, taggedErr, taggedCheckErr
		}
	}

	_, stdoutErr := stdout.WriteTo(cmd.OutOrStdout())
	_, stderrErr := stderr.WriteTo(cmd.ErrOrStderr())
	return errors.Join(err, stdoutErr, stderrErr)
}

func getPortForCheck(cmd *cobra.Command, name string) (string, error) {
	port, err := lookupConfiguredPort(cmd, name)
	if err != nil {
		return "", err
	}
	if !validMaintenancePort(port) {
		return "", errors.New("wgk port get must return a five-digit port (10000-65535)")
	}
	return port, nil
}

func runPortGenerate(cmd *cobra.Command, _ []string) error {
	initial, err := cmd.Flags().GetBool(portGenerateInitialFlag)
	if err != nil {
		return err
	}
	if initial {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), randomPortExcluding(0))
		return err
	}
	name, err := resolveInterface()
	if err != nil {
		return err
	}
	port, err := generateConfiguredPort(cmd, name)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), port)
	return err
}

func generateConfiguredPort(cmd *cobra.Command, name string) (string, error) {
	var output bytes.Buffer
	if err := readConfiguredPort(cmd, name, &output); err != nil {
		return "", err
	}
	current := decimalListenPort(bytes.TrimRight(output.Bytes(), "\n"))
	if current == 0 {
		return "", errors.New("configured listen port must be a decimal port (1-65535)")
	}
	return strconv.Itoa(randomPortExcluding(current)), nil
}

func randomPortExcluding(current int) int {
	const count = maxMaintenancePort - minMaintenancePort + 1
	if current < minMaintenancePort || current > maxMaintenancePort {
		return minMaintenancePort + rand.IntN(count)
	}
	// Draw one fewer value and skip the current port to keep the result uniform.
	port := minMaintenancePort + rand.IntN(count-1)
	if port >= current {
		port++
	}
	return port
}

func lookupConfiguredPort(cmd *cobra.Command, name string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find wgk for port lookup: %w", err)
	}
	process := exec.CommandContext(cmd.Context(), executable, "port", "get", "--"+portGetInterfaceFlag+"="+name)
	process.Stdin = cmd.InOrStdin()
	process.Stderr = cmd.ErrOrStderr()
	output, err := process.Output()
	if err != nil {
		return "", preserveProcessExit(fmt.Errorf("run wgk port get: %w", err))
	}
	return strings.TrimRight(string(output), "\n"), nil
}

func validMaintenancePort(port string) bool {
	number, err := strconv.ParseUint(port, 10, 16)
	return len(port) == 5 && err == nil && number >= minMaintenancePort
}
