package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultBuildGo = "/usr/lib/go-1.26/bin/go" // The default GO in Makefile.

func addToolEnvironment(ctx context.Context, values map[string]string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	goEnv := []string{"GOTOOLCHAIN=local", "GOWORK=off", "GOROOT="}
	pathGo := recordToolPath(values, "tool.go", "go")
	values["tool.go.version"] = firstToolOutputLine(runToolProbe(ctx, pathGo, goEnv, "version"))

	buildGo, overridden := os.LookupEnv("GO")
	if !overridden {
		buildGo = defaultBuildGo
	}
	values["tool.build_go.requested"] = buildGo
	buildGoPath := recordToolPath(values, "tool.build_go", buildGo)
	if buildGoPath == pathGo {
		values["tool.build_go.version"] = values["tool.go.version"]
	} else {
		values["tool.build_go.version"] = firstToolOutputLine(runToolProbe(ctx, buildGoPath, goEnv, "version"))
	}
	keys := []string{
		"GOHOSTOS", "GOHOSTARCH", "GOOS", "GOARCH", "GOAMD64", "GOARM64", "CGO_ENABLED", "GOEXPERIMENT", "GOFIPS140",
		"GOROOT", "GOENV", "GOPATH", "GOCACHE", "GOMODCACHE", "GOTMPDIR", "GOTOOLDIR",
	}
	output := runToolProbe(ctx, buildGoPath, goEnv, append([]string{"env", "-json"}, keys...)...)
	var goSettings map[string]string
	if json.Unmarshal(output, &goSettings) != nil {
		goSettings = nil
	}
	for _, key := range keys {
		values["tool.build_go.env."+key] = goSettings[key]
	}

	dpkg := recordToolPath(values, "tool.dpkg", "dpkg")
	values["host.dpkg_arch"] = firstToolOutputLine(runToolProbe(ctx, dpkg, nil, "--print-architecture"))
	values["tool.dpkg.version"] = firstToolOutputLine(runToolProbe(ctx, dpkg, nil, "--version"))

	for _, tool := range []struct{ name, versionFlag string }{
		{"apt-get", "--version"},
		{"bash", "--version"},
		{"git", "--version"},
		{"install", "--version"},
		{"ip", "-V"},
		{"iptables", "--version"},
		{"make", "--version"},
		{"mv", "--version"},
		{"ss", "--version"},
		{"systemctl", "--version"},
		{"wg", "--version"},
	} {
		prefix := "tool." + tool.name
		path := recordToolPath(values, prefix, tool.name)
		values[prefix+".version"] = firstToolOutputLine(runToolProbe(ctx, path, nil, tool.versionFlag))
	}
	for _, name := range []string{"apt-cache", "awk", "dirname", "mktemp", "rm", "sudo", "tee", "uname", "wg-quick"} {
		recordToolPath(values, "tool."+name, name)
	}
}

func recordToolPath(values map[string]string, prefix, name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		path = ""
	}
	values[prefix+".path"] = path
	return path
}

func runToolProbe(ctx context.Context, path string, env []string, args ...string) []byte {
	if path == "" || ctx.Err() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	cmd.Env = append(cmd.Env, env...)
	cmd.WaitDelay = 100 * time.Millisecond
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return output
}

func firstToolOutputLine(output []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	return strings.TrimSpace(line)
}
