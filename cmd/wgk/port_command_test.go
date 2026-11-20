package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/spf13/cobra"
)

func TestCheckIPTablesPort(t *testing.T) {
	const untaggedArgs = "-C INPUT -p udp --dport 51820 -j ACCEPT"
	const taggedArgs = untaggedArgs + " -m comment --comment wgk-wg-2.alt"
	for _, tc := range []struct {
		name                   string
		untagged, tagged, want int
		wantTagged             bool
		wantResult             string
	}{
		{name: "untagged rule", wantResult: "untagged"},
		{name: "tagged rule", untagged: 1, wantTagged: true, wantResult: "tagged"},
		{name: "missing rules", untagged: 1, tagged: 1, want: 1, wantTagged: true, wantResult: "untagged"},
		{name: "untagged query failure", untagged: 2, want: 2, wantResult: "untagged"},
		{name: "tagged query failure", untagged: 1, tagged: 2, want: 2, wantTagged: true, wantResult: "tagged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			calls := filepath.Join(bin, "iptables.args")
			for name, script := range map[string]string{
				"iptables": `#!/bin/sh
printf '%s\n' "$*" >> "$WGK_IPTABLES_CALLS"
if [ "$#" -gt 8 ]; then
	printf 'tagged output\n'
	printf 'tagged diagnostic\n' >&2
	exit "$WGK_IPTABLES_TAGGED_EXIT"
fi
printf 'untagged output\n'
printf 'untagged diagnostic\n' >&2
exit "$WGK_IPTABLES_UNTAGGED_EXIT"
`,
				"sudo": "#!/bin/sh\nshift\nexec \"$@\"\n",
			} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)
			t.Setenv("WGK_IPTABLES_CALLS", calls)
			t.Setenv("WGK_IPTABLES_UNTAGGED_EXIT", strconv.Itoa(tc.untagged))
			t.Setenv("WGK_IPTABLES_TAGGED_EXIT", strconv.Itoa(tc.tagged))

			var stdout, stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			err := preserveProcessExit(checkIPTablesPort(cmd, "wg-2.alt", "51820"))
			var wantErr error
			if tc.want != 0 {
				wantErr = exitStatus(tc.want)
			}
			if !errors.Is(err, wantErr) {
				t.Errorf("check error = %v, want %v", err, wantErr)
			}
			if got := stdout.String(); got != tc.wantResult+" output\n" {
				t.Errorf("stdout = %q, want %s output", got, tc.wantResult)
			}
			if got := stderr.String(); got != tc.wantResult+" diagnostic\n" {
				t.Errorf("stderr = %q, want %s diagnostic", got, tc.wantResult)
			}
			wantArgs := untaggedArgs + "\n"
			if tc.wantTagged {
				wantArgs += taggedArgs + "\n"
			}
			checkServiceCall(t, calls, wantArgs)
		})
	}
}
