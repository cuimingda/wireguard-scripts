package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

func runPrivileged(cmd *cobra.Command, executable string, args ...string) error {
	if os.Geteuid() != 0 {
		args = append([]string{"--", executable}, args...)
		executable = "sudo"
	}
	process := exec.CommandContext(cmd.Context(), executable, args...)
	process.Stdin = cmd.InOrStdin()
	process.Stdout = cmd.OutOrStdout()
	process.Stderr = cmd.ErrOrStderr()
	return process.Run()
}

func preserveProcessExit(err error) error {
	var child *exec.ExitError
	if !errors.As(err, &child) {
		return err
	}
	if status, ok := child.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return exitStatus(128 + int(status.Signal()))
	}
	return exitStatus(child.ExitCode())
}
