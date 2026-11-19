package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPortSetWithoutPortShowsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"port", "set", "--help"}, nil, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("port set --help exited %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "wgk port set <port>") {
		t.Fatalf("missing port set usage in %q", stdout.String())
	}
	checkStatus(t, []string{"port", "set"}, stdout.String(), "", 0)
}

func TestPortSetChecksInputBeforeStatus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
	checkStatus(t, []string{"port", "set", "51820", "51821"}, "", "received 2", 2)
	for _, port := range []string{"9999", "09999", "65536", "+1234", "1_000", "abcde"} {
		checkStatus(t, []string{"port", "set", "--", port}, "", "five-digit decimal number", 2)
	}
	if _, err := os.Stat(calls); !os.IsNotExist(err) {
		t.Fatalf("unexpected systemctl invocation, log lookup returned %v", err)
	}

	checkStatus(t, []string{"port", "set", "10000"}, "", "is active", 2)
	checkQuery(t, calls, "wg0")
}

func TestPortSetConfirmation(t *testing.T) {
	const original = "[Interface]\nListenPort = 51820\nPrivateKey = sensitive\n" +
		"PostUp = iptables -I INPUT -p udp --dport 51820 -j ACCEPT\n" +
		"PostDown = iptables -D INPUT -p udp --dport 51820 -j ACCEPT\n"
	for _, tc := range []struct {
		name       string
		answer     string
		activate   bool
		force      bool
		wantChange bool
	}{
		{name: "default yes", answer: "\n", wantChange: true},
		{name: "explicit no", answer: "n\n"},
		{name: "unrecognized answer", answer: "x\n"},
		{name: "whitespace is not an empty answer", answer: "  \n"},
		{name: "EOF"},
		{name: "yes without a completed answer", answer: "yes"},
		{name: "explicit yes", answer: "Yes\n", wantChange: true},
		{name: "service active at commit", answer: "y\n", activate: true},
		{name: "force", force: true, wantChange: true},
		{name: "force still checks service at commit", force: true, activate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "wg-test.conf")
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			calls := stubSystemctl(t, "LoadState=loaded\nActiveState=inactive\n", "0")
			var preview, stdout bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cmd.SetOut(&stdout)
			cmd.SetErr(io.Discard)
			activate := func() {
				t.Setenv("WGK_TEST_PROPERTIES", "LoadState=loaded\nActiveState=active\n")
			}
			if tc.force && tc.activate {
				cmd.SetOut(portSetWriteFunc(func(p []byte) (int, error) {
					activate()
					return stdout.Write(p)
				}))
			}
			openTerminal := func() (io.ReadWriteCloser, error) {
				if tc.force {
					t.Fatal("the forced change tried to open a terminal")
				}
				if tc.activate {
					activate()
				}
				return testPortTerminal{Reader: strings.NewReader(tc.answer), Writer: &preview}, nil
			}
			err := setPort(cmd, "wg-quick@wg-test.service", path, "51821", tc.force, openTerminal)
			if tc.activate {
				if err == nil || !strings.Contains(err.Error(), "is active") {
					t.Fatalf("setPort returned %v, want active service error", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			checkQuery(t, calls, "wg-test")

			previewText := preview.String()
			if tc.force {
				previewText = stdout.String()
			}
			for _, detail := range []string{path, "51820 -> 51821", "--dport 51820", "--dport 51821"} {
				if !strings.Contains(previewText, detail) {
					t.Errorf("preview = %q, missing %q", previewText, detail)
				}
			}
			if strings.Contains(previewText, "sensitive") {
				t.Errorf("preview contains private key: %q", previewText)
			}
			if gotPrompt := strings.Contains(previewText, "[Y/n]"); gotPrompt != !tc.force {
				t.Errorf("confirmation prompt presence = %t, force = %t", gotPrompt, tc.force)
			}
			if wantCancel := !tc.wantChange && !tc.activate; strings.Contains(previewText, "Port change cancelled.") != wantCancel {
				t.Errorf("cancellation message differs from expected state: %q", previewText)
			}
			want := original
			if tc.wantChange {
				want = strings.ReplaceAll(original, "51820", "51821")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want {
				t.Errorf("configuration = %q, %v, want %q", got, err, want)
			}
		})
	}
}

func TestPortSetForwardsForceThroughSudo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("sudo forwarding runs only as a non-root user")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	calls := stubSystemctl(t, "LoadState=loaded\nActiveState=inactive\n", "0")
	sudoCalls := filepath.Join(filepath.Dir(calls), "sudo.args")
	t.Setenv("WGK_TEST_SUDO_CALLS", sudoCalls)
	if err := os.WriteFile(filepath.Join(filepath.Dir(calls), "sudo"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WGK_TEST_SUDO_CALLS\"\nexit 37\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--force", "-f"} {
		checkStatus(t, []string{"port", "set", "51821", flag}, "", "", 37)
		output, err := os.ReadFile(sudoCalls)
		if err != nil {
			t.Fatal(err)
		}
		want := "--\n" + executable + "\nport\nset-interface\n--force\n--\nwg0\n51821\n"
		if string(output) != want {
			t.Errorf("%s sudo arguments = %q, want %q", flag, output, want)
		}
	}
}

type portSetWriteFunc func([]byte) (int, error)

func (write portSetWriteFunc) Write(p []byte) (int, error) { return write(p) }

type testPortTerminal struct {
	io.Reader
	io.Writer
}

func (testPortTerminal) Close() error { return nil }
