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

func TestPortSetChecksInputBeforeStatus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	calls := stubSystemctl(t, "LoadState=loaded\nActiveState=active\n", "0")
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
		wantChange bool
	}{
		{name: "default no", answer: "\n"},
		{name: "yes without a completed answer", answer: "yes"},
		{name: "explicit yes", answer: "Yes\n", wantChange: true},
		{name: "service active at commit", answer: "y\n", activate: true},
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
			openTerminal := func() (io.ReadWriteCloser, error) {
				if tc.activate {
					t.Setenv("WGK_TEST_PROPERTIES", "LoadState=loaded\nActiveState=active\n")
				}
				return testPortTerminal{Reader: strings.NewReader(tc.answer), Writer: &preview}, nil
			}
			err := setPort(cmd, "wg-quick@wg-test.service", path, "51821", openTerminal)
			if tc.activate {
				if err == nil || !strings.Contains(err.Error(), "is active") {
					t.Fatalf("setPort returned %v, want active service error", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			checkQuery(t, calls, "wg-test")

			for _, detail := range []string{path, "51820 -> 51821", "--dport 51820", "--dport 51821", "[y/N]"} {
				if !strings.Contains(preview.String(), detail) {
					t.Errorf("preview = %q, missing %q", preview.String(), detail)
				}
			}
			if strings.Contains(preview.String(), "sensitive") {
				t.Errorf("preview contains private key: %q", preview.String())
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

type testPortTerminal struct {
	io.Reader
	io.Writer
}

func (testPortTerminal) Close() error { return nil }
