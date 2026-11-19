package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var errInvalidSetPort = errors.New("port must be a five-digit decimal number from 10000 to 65535")

func runPortSet(cmd *cobra.Command, args []string) error {
	port := args[0]
	if !validMaintenancePort(port) {
		return errInvalidSetPort
	}
	name, err := resolveInterface()
	if err != nil {
		return err
	}
	unit := wireGuardServiceUnit(name)
	if os.Geteuid() == 0 {
		return setPort(cmd, unit, wireGuardConfigPath(name), port, openPortSetTerminal)
	}
	if err := checkPortSetInactive(cmd, unit); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find wgk for privileged port change: %w", err)
	}
	return preserveProcessExit(runPrivileged(cmd, executable, "port", "set-interface", "--", name, port))
}

func runPrivilegedPortSet(cmd *cobra.Command, args []string) error {
	name, port := args[0], args[1]
	if os.Geteuid() != 0 {
		return errors.New("privileged port change requires root")
	}
	if !validInterfaceName(name) {
		return errors.New("invalid interface name for privileged port change")
	}
	if !validMaintenancePort(port) {
		return errInvalidSetPort
	}
	return setPort(cmd, wireGuardServiceUnit(name), wireGuardConfigPath(name), port, openPortSetTerminal)
}

func checkPortSetInactive(cmd *cobra.Command, unit string) error {
	state, err := serviceStatus(cmd.Context(), unit, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if state != "inactive" {
		return fmt.Errorf("%s is %s. Run wgk service down before changing the port", unit, state)
	}
	return nil
}

func setPort(cmd *cobra.Command, unit, path, port string, openTerminal func() (io.ReadWriteCloser, error)) error {
	checkInactive := func() error { return checkPortSetInactive(cmd, unit) }
	if err := checkInactive(); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open WireGuard configuration directory: %w", err)
	}
	defer root.Close()

	name := filepath.Base(path)
	original, err := readPortConfig(root, name)
	if err != nil {
		return fmt.Errorf("read WireGuard configuration %s: %w", path, err)
	}
	change, err := planListenPortChange(original.data, port)
	if err != nil {
		return fmt.Errorf("prepare port change for %s: %w", path, err)
	}
	if len(change.lines) == 0 {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "ListenPort is already %s in %s\n", port, path)
		return err
	}

	terminal, err := openTerminal()
	if err != nil {
		return fmt.Errorf("port change confirmation requires a terminal: %w", err)
	}
	defer terminal.Close()
	confirmed, err := confirmPortChange(terminal, path, port, change)
	if err != nil {
		return fmt.Errorf("confirm port change: %w", err)
	}
	if !confirmed {
		_, err := fmt.Fprintln(terminal, "Port change cancelled.")
		return err
	}

	if err := commitPortConfig(root, name, original, change.updated, checkInactive); err != nil {
		return fmt.Errorf("update WireGuard configuration %s: %w", path, err)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "Updated %s (ListenPort %d -> %s)\n", path, change.beforePort, port)
	return err
}

func openPortSetTerminal() (io.ReadWriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

func confirmPortChange(terminal io.ReadWriter, path, port string, change listenPortChange) (bool, error) {
	var preview strings.Builder
	fmt.Fprintf(&preview, "Configuration %s\nListenPort %d -> %s\n", path, change.beforePort, port)
	for _, line := range change.lines {
		fmt.Fprintf(&preview, "Line %d\n- %s\n+ %s\n", line.lineNumber, line.before, line.after)
	}
	preview.WriteString("\nApply these changes? [y/N] ")
	if _, err := io.WriteString(terminal, preview.String()); err != nil {
		return false, err
	}
	answer, err := bufio.NewReader(terminal).ReadString('\n')
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
