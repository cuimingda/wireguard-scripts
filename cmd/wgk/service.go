package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

func resolveServiceUnit() (string, error) {
	name, err := resolveInterface()
	if err != nil {
		return "", err
	}
	return "wg-quick@" + name + ".service", nil
}

func newServiceCommand(name, description, action string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			unit, err := resolveServiceUnit()
			if err != nil {
				return err
			}

			executable := "systemctl"
			args := []string{action, "--", unit}
			if os.Geteuid() != 0 {
				executable = "sudo"
				args = append([]string{"--", "systemctl"}, args...)
			}
			process := exec.CommandContext(cmd.Context(), executable, args...)
			process.Stdin = cmd.InOrStdin()
			process.Stdout = cmd.OutOrStdout()
			process.Stderr = cmd.ErrOrStderr()
			if err := process.Run(); err != nil {
				var child *exec.ExitError
				if !errors.As(err, &child) {
					return fmt.Errorf("could not %s %s: %w", action, unit, err)
				}
				if status, ok := child.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					return exitStatus(128 + int(status.Signal()))
				}
				return exitStatus(child.ExitCode())
			}
			return nil
		},
	}
}
