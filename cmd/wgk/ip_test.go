package main

import (
	"bytes"
	"context"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestServerIPScope(t *testing.T) {
	for _, test := range []struct {
		address string
		want    string
	}{
		{address: "1.1.1.1", want: "public"},
		{address: "2606:4700:4700::1111", want: "public"},
		{address: "10.0.0.2", want: "private"},
		{address: "172.31.0.2", want: "private"},
		{address: "192.168.0.2", want: "private"},
		{address: "100.64.0.2", want: "private"},
		{address: "100.128.0.2", want: "public"},
		{address: "fd42:42:42::1", want: "private"},
		{address: "127.0.0.1"},
		{address: "::1"},
		{address: "169.254.0.2"},
		{address: "fe80::1"},
		{address: "224.0.0.1"},
		{address: "ff02::1"},
		{address: "192.0.2.1"},
		{address: "198.18.0.2"},
		{address: "2001:db8::1"},
		{address: "3fff::1"},
	} {
		t.Run(test.address, func(t *testing.T) {
			if got := serverIPScope(netip.MustParseAddr(test.address)); got != test.want {
				t.Errorf("scope = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIPListCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := run(context.Background(), []string{"ip", "list"}, nil, &stdout, &stderr)
	if status != 0 || stderr.Len() != 0 {
		t.Fatalf("ip list returned %d with stderr %q", status, stderr.String())
	}
	header, _, _ := strings.Cut(stdout.String(), "\n")
	if want := []string{"SCOPE", "FAMILY", "INTERFACE", "ADDRESS"}; !slices.Equal(strings.Fields(header), want) {
		t.Errorf("ip list header = %q, want %v", header, want)
	}
}
