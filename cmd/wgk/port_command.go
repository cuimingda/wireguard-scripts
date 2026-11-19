package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
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
	command.AddCommand(
		&cobra.Command{
			Use:   "get",
			Short: "Print the configured WireGuard listen port",
			Args:  cobra.NoArgs,
			RunE:  runPortGet,
		},
		&cobra.Command{
			Use:   "set <port>",
			Short: "Change the WireGuard listen port after confirmation",
			Args:  cobra.ExactArgs(1),
			RunE:  runPortSet,
		},
		&cobra.Command{
			Use:    "read <interface>",
			Hidden: true,
			Args:   cobra.ExactArgs(1),
			RunE:   runPrivilegedPortRead,
		},
		&cobra.Command{
			Use:    "set-interface <interface> <port>",
			Hidden: true,
			Args:   cobra.ExactArgs(2),
			RunE:   runPrivilegedPortSet,
		},
		newPortCheckCommand("udp", "List local UDP sockets for the WireGuard port", "ss", func(port string) []string {
			return []string{"-H", "-uln", "sport = :" + port}
		}),
		newPortCheckCommand("iptables", "Check the UDP INPUT rule for the WireGuard port", "iptables", func(port string) []string {
			return []string{"-C", "INPUT", "-p", "udp", "--dport", port, "-j", "ACCEPT"}
		}),
	)
	return command
}

func runPortGet(cmd *cobra.Command, _ []string) error {
	name, err := resolveInterface()
	if err != nil {
		return err
	}
	path := wireGuardConfigPath(name)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrPermission) && os.Geteuid() != 0 {
		executable, err := os.Executable()
		if err != nil {
			return fmt.Errorf("find wgk for privileged port lookup: %w", err)
		}
		if err := runPrivileged(cmd, executable, "port", "read", "--", name); err != nil {
			return preserveProcessExit(fmt.Errorf("read listen port from %s: %w", path, err))
		}
		return nil
	}
	if err != nil {
		return err
	}
	return printListenPort(cmd, path, data)
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
	return printListenPort(cmd, path, data)
}

func printListenPort(cmd *cobra.Command, path string, data []byte) error {
	port, err := parseListenPort(data)
	if err != nil {
		return fmt.Errorf("read listen port from %s: %w", path, err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), port)
	return err
}

func newPortCheckCommand(name, description, executable string, commandArgs func(string) []string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			port, err := getPortForCheck(cmd)
			if err != nil {
				return err
			}
			if err := runPrivileged(cmd, executable, commandArgs(port)...); err != nil {
				return preserveProcessExit(fmt.Errorf("could not run %s: %w", executable, err))
			}
			return nil
		},
	}
}

func getPortForCheck(cmd *cobra.Command) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find wgk for port lookup: %w", err)
	}
	process := exec.CommandContext(cmd.Context(), executable, "port", "get")
	process.Stdin = cmd.InOrStdin()
	process.Stderr = cmd.ErrOrStderr()
	output, err := process.Output()
	if err != nil {
		return "", preserveProcessExit(fmt.Errorf("run wgk port get: %w", err))
	}
	port := strings.TrimRight(string(output), "\n")
	if !validMaintenancePort(port) {
		return "", errors.New("wgk port get must return a five-digit port (10000-65535)")
	}
	return port, nil
}

func validMaintenancePort(port string) bool {
	number, err := strconv.ParseUint(port, 10, 16)
	return len(port) == 5 && err == nil && number >= 10000
}
