package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRandomCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "random",
		Short: "Change to a random WireGuard port and start the service",
		Args:  cobra.NoArgs,
		RunE:  runRandom,
	}
}

func runRandom(cmd *cobra.Command, _ []string) error {
	name, err := resolveInterface()
	if err != nil {
		return err
	}
	port, err := generateConfiguredPort(cmd, name)
	if err != nil {
		return err
	}

	// Check for a terminal before stopping the service for an interactive change.
	terminal, err := openPortSetTerminal()
	if err != nil {
		return fmt.Errorf("port change confirmation requires a terminal: %w", err)
	}
	if err := terminal.Close(); err != nil {
		return err
	}

	unit := wireGuardServiceUnit(name)
	if err := runServiceAction(cmd, unit, "stop"); err != nil {
		return err
	}
	if err := changeConfiguredPort(cmd, name, port, false); err != nil {
		return err
	}
	return runServiceAction(cmd, unit, "start")
}
