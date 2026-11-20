package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

func main() {
	if err := checkUbuntu(runtime.GOOS, os.ReadFile); err != nil {
		fmt.Fprintf(os.Stderr, "wgk: %v\n", err)
		os.Exit(2)
	}
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

	root.AddCommand(
		newConfigCommand(),
		newEnvCommand(),
		newInstallCommand(),
		newIPCommand(),
		newPortCommand(),
		newRandomCommand(),
		newServiceCommand(),
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
