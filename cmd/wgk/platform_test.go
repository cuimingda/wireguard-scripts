package main

import (
	"io/fs"
	"strings"
	"testing"
)

func TestCheckUbuntu(t *testing.T) {
	for _, tc := range []struct {
		name, goos, primary, fallback, wantError string
		primaryErr, fallbackErr                  error
	}{
		{name: "Ubuntu", goos: "linux", primary: "NAME=Ubuntu\nID=ubuntu\nID_LIKE=debian\n"},
		{name: "double quotes", goos: "linux", primary: `ID="ubuntu"`},
		{name: "single quotes", goos: "linux", primary: "ID='ubuntu'"},
		{name: "fallback", goos: "linux", primaryErr: fs.ErrNotExist, fallback: "ID=ubuntu\n"},
		{name: "Ubuntu derivative", goos: "linux", primary: "ID=linuxmint\nID_LIKE=ubuntu\n", fallback: "ID=ubuntu\n", wantError: "/etc/os-release"},
		{name: "missing ID", goos: "linux", primary: "# ID=ubuntu\nID_LIKE=ubuntu\n", wantError: "expected ID=ubuntu"},
		{name: "unmatched quote", goos: "linux", primary: `ID="ubuntu`, wantError: "expected ID=ubuntu"},
		{name: "last ID takes precedence", goos: "linux", primary: "ID=ubuntu\nID=debian\n", wantError: "expected ID=ubuntu"},
		{name: "unreadable primary", goos: "linux", primaryErr: fs.ErrPermission, fallback: "ID=ubuntu", wantError: "permission denied"},
		{name: "missing files", goos: "linux", primaryErr: fs.ErrNotExist, fallbackErr: fs.ErrNotExist, wantError: "could not read OS release information"},
		{name: "non-Linux", goos: "darwin", wantError: "detected darwin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkUbuntu(tc.goos, func(path string) ([]byte, error) {
				if tc.goos != "linux" {
					t.Fatalf("unexpected read of %s on %s", path, tc.goos)
				}
				switch path {
				case "/etc/os-release":
					return []byte(tc.primary), tc.primaryErr
				case "/usr/lib/os-release":
					return []byte(tc.fallback), tc.fallbackErr
				default:
					t.Fatalf("unexpected read of %s", path)
					return nil, fs.ErrNotExist
				}
			})
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("checkUbuntu() = %v, want success", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("checkUbuntu() = %v, want diagnostic containing %q", err, tc.wantError)
			}
		})
	}
}
