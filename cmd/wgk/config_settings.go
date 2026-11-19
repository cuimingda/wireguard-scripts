package main

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

func getConfigValue(key string) (any, error) {
	parts, err := configKeyParts(key)
	if err != nil {
		return nil, err
	}
	settings, err := configSettings(configResolved)
	if err != nil {
		return nil, err
	}
	if value, ok := findConfigValue(settings, parts); ok {
		return value, nil
	}
	return nil, fmt.Errorf("configuration key is not set: %s", key)
}

func setConfigValue(key, raw string) error {
	parts, err := configKeyParts(key)
	if err != nil {
		return err
	}
	value, err := parseConfigScalar(raw)
	if err != nil {
		return err
	}
	if parts[0] == "interface" {
		name, ok := value.(string)
		if len(parts) != 1 || !ok || !validInterfaceName(name) {
			return fmt.Errorf("invalid interface name in configuration")
		}
	}
	_, err = withConfigWrite(func(path string) error {
		settings, err := readUserConfig(path)
		if err != nil {
			return err
		}
		if err := assignConfigValue(settings, parts, value); err != nil {
			return err
		}
		return writeUserSettings(path, settings, true)
	})
	return err
}

func unsetConfigValue(key string) error {
	parts, err := configKeyParts(key)
	if err != nil {
		return err
	}
	path, err := userConfigPath()
	if err != nil {
		return err
	}
	missingKey := fmt.Errorf("configuration key is not set in user configuration: %s", key)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return missingKey
	}
	_, err = withConfigWrite(func(path string) error {
		settings, err := readUserConfig(path)
		if err != nil {
			return err
		}
		if !removeConfigValue(settings, parts) {
			return missingKey
		}
		return writeUserSettings(path, settings, true)
	})
	return err
}

func normalizeConfigKeys(settings map[string]any) error {
	for key, value := range settings {
		if err := normalizeConfigValue(value); err != nil {
			return fmt.Errorf("key %q: %w", key, err)
		}
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == key {
			continue
		}
		if _, exists := settings[normalized]; exists {
			return fmt.Errorf("duplicate configuration key: %q", normalized)
		}
		delete(settings, key)
		settings[normalized] = value
	}
	return nil
}

func normalizeConfigValue(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeConfigKeys(typed)
	case map[any]any:
		return fmt.Errorf("configuration mappings must have string keys")
	case []any:
		for _, element := range typed {
			if err := normalizeConfigValue(element); err != nil {
				return err
			}
		}
	}
	return nil
}

func mergeConfigSettings(base, overrides map[string]any) {
	for key, value := range overrides {
		baseMap, baseOK := base[key].(map[string]any)
		overrideMap, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			mergeConfigSettings(baseMap, overrideMap)
		} else {
			base[key] = value
		}
	}
}

func configKeyParts(key string) ([]string, error) {
	parts := strings.Split(strings.ToLower(key), ".")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		if parts[i] == "" {
			return nil, fmt.Errorf("configuration key contains an empty path segment: %q", key)
		}
	}
	return parts, nil
}

func findConfigValue(settings map[string]any, parts []string) (any, bool) {
	value, found := settings[parts[0]]
	if len(parts) == 1 || !found {
		return value, found
	}
	child, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return findConfigValue(child, parts[1:])
}

func assignConfigValue(settings map[string]any, parts []string, value any) error {
	for index, part := range parts[:len(parts)-1] {
		child, exists := settings[part]
		if !exists {
			child = map[string]any{}
			settings[part] = child
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("configuration key is not a mapping: %s", strings.Join(parts[:index+1], "."))
		}
		settings = next
	}
	settings[parts[len(parts)-1]] = value
	return nil
}

func removeConfigValue(settings map[string]any, parts []string) bool {
	key := parts[0]
	value, found := settings[key]
	if !found {
		return false
	}
	if len(parts) == 1 {
		delete(settings, key)
		return true
	}
	child, ok := value.(map[string]any)
	if !ok || !removeConfigValue(child, parts[1:]) {
		return false
	}
	if len(child) == 0 {
		delete(settings, key)
	}
	return true
}

func filterConfigSettings(settings map[string]any, filter string) (map[string]any, error) {
	if strings.TrimSpace(filter) == "" {
		return settings, nil
	}
	parts, err := configKeyParts(filter)
	if err != nil {
		return nil, err
	}
	value, found := findConfigValue(settings, parts)
	if !found {
		return map[string]any{}, nil
	}
	for i := len(parts) - 1; i >= 0; i-- {
		value = map[string]any{parts[i]: value}
	}
	return value.(map[string]any), nil
}

func parseConfigScalar(raw string) (any, error) {
	var document yaml.Node
	if err := decodeConfigYAML([]byte(raw), &document); err != nil {
		return nil, fmt.Errorf("parse configuration scalar: %w", err)
	}
	if len(document.Content) == 0 {
		return nil, nil
	}
	node := document.Content[0]
	if node.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("configuration value must be a YAML scalar")
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return nil, fmt.Errorf("parse configuration scalar: %w", err)
	}
	return value, nil
}
