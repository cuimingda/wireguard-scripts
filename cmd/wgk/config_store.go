package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"go.yaml.in/yaml/v3"
)

//go:embed config/default.yaml
var defaultConfigYAML []byte

type configSource int

const (
	configResolved configSource = iota
	configDefault
	configUser
)

var errNoConfigHome = errors.New("cannot locate user configuration without HOME or an absolute XDG_CONFIG_HOME")

func configHome() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Clean(xdg), nil
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Abs(filepath.Join(home, ".config"))
	}
	return "", errNoConfigHome
}

func userConfigPath() (string, error) {
	home, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "mingda.dev", "wireguard-toolkit", "config.yaml"), nil
}

func configSettings(source configSource) (map[string]any, error) {
	if source == configDefault {
		return readConfigYAML(defaultConfigYAML, "embedded default configuration")
	}
	path, err := userConfigPath()
	if source == configResolved && errors.Is(err, errNoConfigHome) {
		return configSettings(configDefault)
	}
	if err != nil {
		return nil, err
	}
	user, err := readUserConfig(path)
	if err != nil || source == configUser {
		return user, err
	}
	defaults, err := configSettings(configDefault)
	if err != nil {
		return nil, err
	}
	if _, overridden := user["interface"]; !overridden {
		if err := useLegacyInterface(defaults); err != nil {
			return nil, err
		}
	}
	mergeConfigSettings(defaults, user)
	return defaults, nil
}

func useLegacyInterface(settings map[string]any) error {
	home, err := configHome()
	if err != nil {
		return err
	}
	name, err := legacyInterface(home)
	if err != nil {
		return err
	}
	if name != "" {
		settings["interface"] = name
	}
	return nil
}

func readUserConfig(path string) (map[string]any, error) {
	file, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user configuration: %w", err)
	}
	if !file.Mode().IsRegular() {
		return nil, fmt.Errorf("user configuration is not a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read user configuration: %w", err)
	}
	return readConfigYAML(data, path)
}

func readConfigYAML(data []byte, source string) (map[string]any, error) {
	var settings map[string]any
	if err := decodeConfigYAML(data, &settings); err != nil {
		return nil, fmt.Errorf("read configuration %q: %w", source, err)
	}
	if settings == nil {
		return map[string]any{}, nil
	}
	if err := normalizeConfigKeys(settings); err != nil {
		return nil, fmt.Errorf("read configuration %q: %w", source, err)
	}
	return settings, nil
}

func decodeConfigYAML(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("multiple YAML documents are not supported")
	}
	return nil
}

func initUserConfig() (string, error) {
	return withConfigWrite(func(path string) error {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("user configuration already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect user configuration: %w", err)
		}
		settings, err := configSettings(configDefault)
		if err != nil {
			return err
		}
		if err := useLegacyInterface(settings); err != nil {
			return err
		}
		return writeUserSettings(path, settings, false)
	})
}

func ensureUserConfig() (string, error) {
	return withConfigWrite(func(path string) error {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return writeUserConfig(path, nil, false)
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("user configuration is not a regular file: %s", path)
		}
		return nil
	})
}

func withConfigWrite(write func(string) error) (string, error) {
	path, err := userConfigPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create user configuration directory: %w", err)
	}
	lock, err := os.Open(dir)
	if err != nil {
		return "", fmt.Errorf("open user configuration directory: %w", err)
	}
	defer lock.Close()
	// Lock the directory so the lock outlives replacements of config.yaml.
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("lock user configuration directory %q: %w", dir, err)
	}
	if err := write(path); err != nil {
		return "", err
	}
	return path, nil
}

func writeUserSettings(path string, settings map[string]any, replace bool) error {
	data, err := yaml.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode user configuration: %w", err)
	}
	return writeUserConfig(path, data, replace)
}

func writeUserConfig(path string, data []byte, replace bool) error {
	if replace {
		info, err := os.Lstat(path)
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("user configuration is not a regular file: %s", path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect user configuration: %w", err)
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err := temp.Write(data); err != nil {
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if replace {
		if err := os.Rename(temp.Name(), path); err != nil {
			return fmt.Errorf("replace user configuration: %w", err)
		}
		return nil
	}
	if err := os.Link(temp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("user configuration already exists: %s", path)
		}
		return fmt.Errorf("create user configuration: %w", err)
	}
	return nil
}
