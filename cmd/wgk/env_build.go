package main

import (
	"runtime"
	"runtime/debug"
	"strings"
)

func addBuildEnvironment(values map[string]string) {
	values["build.go.version"] = runtime.Version()
	values["build.go.os"] = runtime.GOOS
	values["build.go.arch"] = runtime.GOARCH
	values["build.go.compiler"] = runtime.Compiler

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Path != "" {
		values["build.package.path"] = info.Path
	}
	if info.Main.Path != "" {
		values["build.module.path"] = info.Main.Path
		values["build.module.version"] = info.Main.Version
	}
	for _, dep := range info.Deps {
		version := dep.Version
		if dep.Replace != nil {
			if dep.Replace.Version == "" {
				version += " => (local replacement)"
			} else {
				version += " => " + dep.Replace.Path + "@" + dep.Replace.Version
			}
		}
		values["build.dependency."+dep.Path] = version
	}

	// Raw compiler and linker flags can contain local paths and injected values.
	for _, setting := range info.Settings {
		switch setting.Key {
		case "CGO_ENABLED", "DefaultGODEBUG", "GOEXPERIMENT", "GOFIPS140",
			"GO386", "GOAMD64", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM":
			values["build.go."+setting.Key] = setting.Value
		case "-asan", "-buildmode", "-cover", "-msan", "-race", "-tags", "-trimpath":
			values["build.go."+strings.TrimPrefix(setting.Key, "-")] = setting.Value
		case "-pgo":
			values["build.go.pgo"] = "true"
		case "vcs":
			values["build.vcs.type"] = setting.Value
		case "vcs.revision", "vcs.time", "vcs.modified":
			values["build."+setting.Key] = setting.Value
		}
	}
}
