package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newServiceCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "service",
		Short: "Manage the WireGuard service",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(
		newServiceConfigCommand(),
		newServiceActionCommand("up", "Start the WireGuard service", "start"),
		newServiceActionCommand("down", "Stop the WireGuard service", "stop"),
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the WireGuard service is active",
			Args:  cobra.NoArgs,
			RunE:  runServiceStatus,
		},
	)
	return command
}

func newServiceConfigCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Inspect the WireGuard interface configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the WireGuard interface configuration path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := resolveInterface()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), wireGuardConfigPath(name))
			return err
		},
	})
	return command
}

func runServiceStatus(cmd *cobra.Command, _ []string) error {
	unit, err := resolveServiceUnit()
	if err != nil {
		return err
	}
	state, err := serviceStatus(cmd.Context(), unit, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), state); err != nil {
		return err
	}
	if state == "inactive" {
		return exitStatus(1)
	}
	return nil
}

func resolveServiceUnit() (string, error) {
	name, err := resolveInterface()
	if err != nil {
		return "", err
	}
	return wireGuardServiceUnit(name), nil
}

func wireGuardServiceUnit(name string) string {
	return "wg-quick@" + name + ".service"
}

func newServiceActionCommand(name, description, action string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			unit, err := resolveServiceUnit()
			if err != nil {
				return err
			}

			if err := runPrivileged(cmd, "systemctl", action, "--", unit); err != nil {
				return preserveProcessExit(fmt.Errorf("could not %s %s: %w", action, unit, err))
			}
			return nil
		},
	}
}
