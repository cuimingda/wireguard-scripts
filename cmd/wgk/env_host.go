package main

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func addHostEnvironment(values map[string]string) {
	addOSReleaseEnvironment(values, os.ReadFile)
	values["host.cpu.available"] = strconv.Itoa(runtime.NumCPU())
	values["host.cpu.parallelism"] = strconv.Itoa(runtime.GOMAXPROCS(0))
	values["host.kernel.page_size_bytes"] = strconv.Itoa(os.Getpagesize())

	var name unix.Utsname
	if err := unix.Uname(&name); err == nil {
		values["host.arch"] = unix.ByteSliceToString(name.Machine[:])
		values["host.kernel.name"] = unix.ByteSliceToString(name.Sysname[:])
		values["host.kernel.release"] = unix.ByteSliceToString(name.Release[:])
		values["host.kernel.version"] = unix.ByteSliceToString(name.Version[:])
	}

	if data, err := os.ReadFile("/proc/1/comm"); err == nil {
		values["host.pid1"] = strings.TrimSuffix(string(data), "\n")
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil || errors.Is(err, os.ErrNotExist) {
		values["host.systemd_booted"] = strconv.FormatBool(err == nil)
	}
}

func addOSReleaseEnvironment(values map[string]string, readFile func(string) ([]byte, error)) {
	data, path, err := readOSRelease(readFile)
	if err != nil {
		return
	}
	values["host.os.release_file"] = path
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ID", "ID_LIKE", "NAME", "PRETTY_NAME", "VERSION", "VERSION_ID", "VERSION_CODENAME",
			"UBUNTU_CODENAME", "VARIANT", "VARIANT_ID", "BUILD_ID", "IMAGE_ID", "IMAGE_VERSION":
			values["host.os."+strings.ToLower(key)] = osReleaseValue(value)
		}
	}
}

var osReleaseEscapes = strings.NewReplacer("\\\\", "\\", "\\\"", "\"", "\\$", "$", "\\`", "`")

func osReleaseValue(value string) string {
	if len(value) >= 2 && value[0] == value[len(value)-1] {
		switch value[0] {
		case '\'':
			return value[1 : len(value)-1]
		case '"':
			value = value[1 : len(value)-1]
		}
	}
	return osReleaseEscapes.Replace(value)
}
