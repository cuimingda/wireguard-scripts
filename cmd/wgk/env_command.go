package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

func newEnvCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "env",
		Short: "Print build and runtime diagnostics as key=value lines",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			values := make(map[string]string)
			addHostEnvironment(values)
			addBuildEnvironment(values)
			addRuntimeEnvironment(values)
			addToolEnvironment(cmd.Context(), values)
			return writeEnvironment(cmd.OutOrStdout(), values)
		},
	}
}

func addRuntimeEnvironment(values map[string]string) {
	values["runtime.uid"] = strconv.Itoa(os.Getuid())
	values["runtime.euid"] = strconv.Itoa(os.Geteuid())
	if executable, err := os.Executable(); err == nil {
		values["runtime.executable"] = executable
	}
	if cwd, err := os.Getwd(); err == nil {
		values["runtime.cwd"] = cwd
	}
	if path, err := userConfigPath(); err == nil {
		values["runtime.config.path"] = path
	}
	if name, err := resolveInterface(); err == nil {
		values["runtime.wireguard.interface"] = name
		values["runtime.wireguard.config_path"] = wireGuardConfigPath(name)
		values["runtime.wireguard.unit"] = wireGuardServiceUnit(name)
	}

	for _, name := range []string{
		"PATH", "HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "TMPDIR", "LANG", "LC_ALL", "LC_CTYPE", "TZ",
		"DESTDIR", "GO", "GOOS", "GOARCH", "GOAMD64", "GOARM64", "CGO_ENABLED", "GOEXPERIMENT", "GOFIPS140",
		"GOENV", "GOROOT", "GOPATH", "GOCACHE", "GOMODCACHE", "GOTMPDIR", "GOTOOLCHAIN", "GOWORK",
		"GODEBUG", "GOGC", "GOMAXPROCS", "GOMEMLIMIT", "LD_LIBRARY_PATH",
	} {
		values["env."+name] = os.Getenv(name)
	}
	for _, name := range []string{
		"GOFLAGS", "GOAUTH", "GOPROXY", "GOPRIVATE", "GONOPROXY", "GONOSUMDB", "GOSUMDB", "GOINSECURE",
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy",
	} {
		_, set := os.LookupEnv(name)
		values["env."+name+".set"] = strconv.FormatBool(set)
	}
}

func writeEnvironment(out io.Writer, values map[string]string) error {
	var lines strings.Builder
	for _, key := range slices.Sorted(maps.Keys(values)) {
		value := values[key]
		if !utf8.ValidString(value) || strings.ContainsFunc(value, configControlCharacter) {
			value = strconv.Quote(value)
		}
		fmt.Fprintf(&lines, "%s=%s\n", key, value)
	}
	_, err := io.WriteString(out, lines.String())
	return err
}
