package main

import (
	"strings"
	"testing"
)

func TestParseListenPort(t *testing.T) {
	const invalidPort = "ListenPort must be a decimal port from 1 to 65535"
	const wrongFieldCount = "expected exactly one ListenPort in [Interface]"
	for _, tt := range []struct {
		name, config string
		want         int
		wantError    string
	}{
		{name: "interface field", config: "[Interface]\nListenPort = 51820\n", want: 51820},
		{name: "ignore peers and comments", config: "[Peer]\nListenPort=12345\n\t[INTERFACE] # section\r\n# ListenPort=0\r\n lIsTenPoRt \t= 00012345 # ignored=1\r\n[Peer]\nListenPort=22222", want: 12345},
		{name: "minimum", config: "[Interface]\nListenPort=1", want: 1},
		{name: "maximum", config: "[Interface]\nListenPort=65535", want: 65535},
		{name: "long unrelated line", config: "[Interface]\nIgnored=" + strings.Repeat("x", 64*1024) + "\nListenPort=12345", want: 12345},
		{name: "missing", config: "[Interface]\n[Peer]\nListenPort=12345", wantError: wrongFieldCount},
		{name: "duplicate", config: "[Interface]\nListenPort=\nLISTENPORT=12345", wantError: wrongFieldCount},
		{name: "Unicode key", config: "[Interface]\nLiſtenPort=12345", wantError: wrongFieldCount},
		{name: "no assignment", config: "[Interface]\nListenPort", wantError: invalidPort},
		{name: "zero", config: "[Interface]\nListenPort=000", wantError: invalidPort},
		{name: "overflow", config: "[Interface]\nListenPort=65536", wantError: invalidPort},
		{name: "signed", config: "[Interface]\nListenPort=+12345", wantError: invalidPort},
		{name: "service name", config: "[Interface]\nListenPort=private-value", wantError: invalidPort},
		{name: "NUL byte", config: "[Interface]\nListenPort=12345\nIgnored=\x00", wantError: "configuration contains a NUL byte"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			port, err := parseListenPort([]byte(tt.config))
			if port != tt.want {
				t.Errorf("port = %d, want %d", port, tt.want)
			}
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || err.Error() != tt.wantError {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}
