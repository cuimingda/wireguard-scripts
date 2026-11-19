package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

type exitStatus int

func (status exitStatus) Error() string {
	return fmt.Sprintf("exit status %d", status)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := &cobra.Command{
		Use:           "wgk",
		Short:         "Maintain WireGuard on Ubuntu",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether the WireGuard service is active",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
		},
	})
	root.AddCommand(&cobra.Command{
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
	root.AddCommand(
		newConfigCommand(),
		newPortCommand(),
		newServiceCommand("up", "Start the WireGuard service", "start"),
		newServiceCommand("down", "Stop the WireGuard service", "stop"),
	)
	if err := root.ExecuteContext(ctx); err != nil {
		var status exitStatus
		if errors.As(err, &status) {
			return int(status)
		}
		fmt.Fprintf(stderr, "wgk: %v\n", err)
		return 2
	}
	return 0
}
