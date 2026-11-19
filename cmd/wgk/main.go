package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	root := &cobra.Command{
		Use:           "wgk",
		Short:         "Maintain WireGuard on Ubuntu",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	exitCode := 0
	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether the WireGuard service is active",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := resolveInterface()
			if err != nil {
				return err
			}
			state, err := serviceStatus(cmd.Context(), "wg-quick@"+name+".service", cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), state); err != nil {
				return err
			}
			if state == "inactive" {
				exitCode = 1
			}
			return nil
		},
	})
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "wgk: %v\n", err)
		return 2
	}
	return exitCode
}
