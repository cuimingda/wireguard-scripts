package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Args:  configArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	group.SetFlagErrorFunc(reportConfigError)

	get := configLeaf("get <key>", "Get a resolved configuration value", 1,
		func(cmd *cobra.Command, args []string) error {
			value, err := getConfigValue(args[0])
			if err != nil {
				return err
			}
			return writeConfigValue(cmd.OutOrStdout(), value)
		})
	get.Flags().Bool("show-hidden", false, "show hidden configuration values")

	group.AddCommand(
		newConfigListCommand(),
		get,
		configLeaf("set <key> <value>", "Set a user configuration value", 2,
			func(_ *cobra.Command, args []string) error {
				return setConfigValue(args[0], args[1])
			}),
		configLeaf("unset <key>", "Unset a user configuration value", 1,
			func(_ *cobra.Command, args []string) error {
				return unsetConfigValue(args[0])
			}),
		configLeaf("path", "Print the user configuration file path", 0,
			func(cmd *cobra.Command, _ []string) error {
				path, err := userConfigPath()
				if err != nil {
					return err
				}
				return writeConfigLine(cmd.OutOrStdout(), path)
			}),
		newConfigEditCommand(),
		configLeaf("init", "Create the user configuration file", 0,
			func(cmd *cobra.Command, _ []string) error {
				path, err := initUserConfig()
				if err != nil {
					return err
				}
				return writeConfigLine(cmd.OutOrStdout(), path)
			}),
	)
	return group
}

func newConfigListCommand() *cobra.Command {
	var showDefault, showUser bool
	var filter string
	command := configLeaf("list", "List configuration", 0, func(cmd *cobra.Command, _ []string) error {
		if showDefault && showUser {
			return fmt.Errorf("--default and --user cannot both be set")
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
			return err
		}
		filtered, err := filterConfigSettings(settings, filter)
		if err != nil {
			return err
		}
		return writeConfigList(cmd.OutOrStdout(), filtered)
	})
	command.Flags().BoolVar(&showDefault, "default", false, "list application default configuration")
	command.Flags().BoolVar(&showUser, "user", false, "list user configuration")
	command.Flags().Bool("show-hidden", false, "show hidden configuration values")
	command.Flags().StringVar(&filter, "filter", "", "list only the configuration subtree at this dot-separated path")
	return command
}

func configLeaf(use, short string, count int, execute func(*cobra.Command, []string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  configArgs(cobra.ExactArgs(count)),
		RunE: func(cmd *cobra.Command, args []string) error {
			return reportConfigError(cmd, execute(cmd, args))
		},
	}
}

func configArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		return reportConfigError(cmd, validate(cmd, args))
	}
}

func reportConfigError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", cmd.CommandPath(), err)
	return exitStatus(1)
}
