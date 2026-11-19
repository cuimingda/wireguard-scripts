package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type configResult struct {
	OK       bool    `json:"ok"`
	Data     any     `json:"data"`
	Error    *string `json:"error"`
	ExitCode int     `json:"exit_code"`
}

func newConfigCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Args:  configArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	group.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return writeConfigResult(cmd, nil, err)
	})
	group.PersistentFlags().Bool("api", false, "write the compact JSON result")

	get := configLeaf("get <key>", "Get a resolved configuration value", 1,
		func(_ *cobra.Command, args []string) (any, error) {
			value, err := getConfigValue(args[0])
			return map[string]any{"key": args[0], "value": value}, err
		})
	get.Flags().Bool("show-hidden", false, "show hidden configuration values")

	group.AddCommand(
		newConfigListCommand(),
		get,
		configLeaf("set <key> <value>", "Set a user configuration value", 2,
			func(_ *cobra.Command, args []string) (any, error) {
				return map[string]string{"key": args[0]}, setConfigValue(args[0], args[1])
			}),
		configLeaf("unset <key>", "Unset a user configuration value", 1,
			func(_ *cobra.Command, args []string) (any, error) {
				return map[string]string{"key": args[0]}, unsetConfigValue(args[0])
			}),
		configLeaf("path", "Print the user configuration file path", 0,
			func(_ *cobra.Command, _ []string) (any, error) {
				path, err := userConfigPath()
				return map[string]string{"path": path}, err
			}),
		newConfigEditCommand(),
		configLeaf("init", "Create the user configuration file", 0,
			func(_ *cobra.Command, _ []string) (any, error) {
				path, err := initUserConfig()
				return map[string]string{"path": path}, err
			}),
	)
	return group
}

func newConfigListCommand() *cobra.Command {
	var showDefault, showUser bool
	var filter string
	command := configLeaf("list", "List configuration", 0, func(_ *cobra.Command, _ []string) (any, error) {
		if showDefault && showUser {
			return nil, fmt.Errorf("config list: --default and --user cannot both be set")
		}
		source := configResolved
		switch {
		case showDefault:
			source = configDefault
		case showUser:
			source = configUser
		}
		settings, err := configSettings(source)
		if err != nil {
			return nil, err
		}
		return filterConfigSettings(settings, filter)
	})
	command.Flags().BoolVar(&showDefault, "default", false, "list application default configuration")
	command.Flags().BoolVar(&showUser, "user", false, "list user configuration")
	command.Flags().Bool("show-hidden", false, "show hidden configuration values")
	command.Flags().StringVar(&filter, "filter", "", "list only the configuration subtree at this dot-separated path")
	return command
}

func configLeaf(use, short string, count int, execute func(*cobra.Command, []string) (any, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  configArgs(count),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := execute(cmd, args)
			return writeConfigResult(cmd, data, err)
		},
	}
}

func configArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == count {
			return nil
		}
		label := strings.TrimPrefix(cmd.CommandPath(), "wgk ")
		if count == 0 {
			return writeConfigResult(cmd, nil, fmt.Errorf("%s: does not accept positional arguments: %v", label, args))
		}
		noun := "arguments"
		if count == 1 {
			noun = "argument"
		}
		return writeConfigResult(cmd, nil, fmt.Errorf("%s: expected exactly %d positional %s, got %d", label, count, noun, len(args)))
	}
}

func writeConfigResult(cmd *cobra.Command, data any, commandErr error) error {
	result := configResult{OK: true, Data: data}
	if commandErr != nil {
		message := commandErr.Error()
		result = configResult{Error: &message, ExitCode: 1}
	}
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
		return fmt.Errorf("write config result: %w", err)
	}
	if commandErr != nil {
		return exitStatus(1)
	}
	return nil
}
