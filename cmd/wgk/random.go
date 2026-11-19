package main

import "github.com/spf13/cobra"

func newRandomCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "random",
		Short: "Change to a random WireGuard port and restart the service without prompting",
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

	unit := wireGuardServiceUnit(name)
	if err := runServiceAction(cmd, unit, "stop"); err != nil {
		return err
	}
	if err := changeConfiguredPort(cmd, name, port, true); err != nil {
		return err
	}
	return runServiceAction(cmd, unit, "start")
}
