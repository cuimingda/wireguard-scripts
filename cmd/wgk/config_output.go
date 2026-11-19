package main

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

func writeConfigList(out io.Writer, settings map[string]any) error {
	values := make(map[string]any)
	flattenConfigValues(values, "", settings)
	var lines strings.Builder
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value, err := inlineConfigYAML(values[key])
		if err != nil {
			return fmt.Errorf("render configuration key %q: %w", key, err)
		}
		fmt.Fprintf(&lines, "%s=%s\n", key, value)
	}
	_, err := io.WriteString(out, lines.String())
	return err
}

func flattenConfigValues(values map[string]any, prefix string, settings map[string]any) {
	for key, value := range settings {
		path := prefix + configOutputKey(key)
		if nested, ok := value.(map[string]any); ok && len(nested) != 0 {
			flattenConfigValues(values, path+".", nested)
			continue
		}
		values[path] = value
	}
}

var configKeyDelimiters = strings.NewReplacer(".", `\.`, "=", `\=`)

func configOutputKey(key string) string {
	// Escape record delimiters and control characters within a path segment.
	quoted := strconv.Quote(key)
	return configKeyDelimiters.Replace(quoted[1 : len(quoted)-1])
}

func writeConfigValue(out io.Writer, value any) error {
	text, isString := value.(string)
	if !isString || !utf8.ValidString(text) || strings.ContainsFunc(text, configControlCharacter) {
		var err error
		text, err = inlineConfigYAML(value)
		if err != nil {
			return fmt.Errorf("render configuration value: %w", err)
		}
	}
	return writeConfigLine(out, text)
}

func writeConfigLine(out io.Writer, text string) error {
	_, err := fmt.Fprintln(out, text)
	return err
}

func inlineConfigYAML(value any) (string, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return "", err
	}
	setInlineConfigStyle(&node)
	data, err := yaml.Marshal(&node)
	return strings.TrimSuffix(string(data), "\n"), err
}

func setInlineConfigStyle(node *yaml.Node) {
	switch node.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		node.Style |= yaml.FlowStyle
		for _, child := range node.Content {
			setInlineConfigStyle(child)
		}
	case yaml.ScalarNode:
		if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.ContainsFunc(node.Value, configControlCharacter) {
			node.Style = node.Style&yaml.TaggedStyle | yaml.DoubleQuotedStyle
		}
	}
}

func configControlCharacter(char rune) bool {
	return unicode.IsControl(char) || char == '\u2028' || char == '\u2029'
}
