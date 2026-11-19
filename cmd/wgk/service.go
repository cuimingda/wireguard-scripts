package main

import (
	"fmt"

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

			if err := runPrivileged(cmd, "systemctl", action, "--", unit); err != nil {
				return preserveProcessExit(fmt.Errorf("could not %s %s: %w", action, unit, err))
			}
			return nil
		},
	}
}
