package main

import (
	"fmt"
	"os/exec"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func newConfigEditCommand() *cobra.Command {
	var editor string
	command := configLeaf("edit", "Edit the user configuration file", 0, func(cmd *cobra.Command, _ []string) (any, error) {
		args, err := configEditorArgs(editor)
		if err != nil {
			return nil, err
		}
		path, err := ensureUserConfig()
		if err != nil {
			return nil, err
		}
		process := exec.CommandContext(cmd.Context(), args[0], append(args[1:], path)...)
		process.Stdin = cmd.InOrStdin()
		process.Stdout = cmd.ErrOrStderr()
		process.Stderr = cmd.ErrOrStderr()
		if err := process.Run(); err != nil {
			return nil, fmt.Errorf("config edit: run editor %q: %w", args[0], err)
		}
		return map[string]string{"path": path}, nil
	})
	command.Flags().StringVar(&editor, "editor", "vi", "editor command used to edit the user configuration file")
	return command
}

// configEditorArgs supports quoted arguments without expanding shell syntax.
func configEditorArgs(editor string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote rune
	var started, escaped bool
	for _, char := range editor {
		switch {
		case escaped:
			word.WriteRune(char)
			escaped = false
		case char == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
		case unicode.IsSpace(char):
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
			continue
		default:
			word.WriteRune(char)
		}
		started = true
	}
	if quote != 0 {
		return nil, fmt.Errorf("config edit: --editor contains an unterminated quote")
	}
	if escaped {
		word.WriteByte('\\')
	}
	if started {
		args = append(args, word.String())
	}
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("config edit: --editor requires an executable")
	}
	return args, nil
}
