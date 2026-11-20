package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlanListenPortChange(t *testing.T) {
	const (
		iptablesUp     = "iptables -I INPUT -p udp --dport "
		iptablesDown   = "iptables -D INPUT -p udp --dport "
		ip6tablesUp    = "ip6tables -I INPUT -p udp --dport "
		ip6tablesDown  = "ip6tables -D INPUT -p udp --dport "
		ownedArgs      = " INPUT -p udp --dport 51820 -j ACCEPT -m comment --comment wgk-wg0"
		ownedUp        = "PostUp = iptables -C" + ownedArgs + " || iptables -I" + ownedArgs
		ownedDown      = "PostDown = if ip6tables -C" + ownedArgs + "; then ip6tables -D" + ownedArgs + "; fi"
		firewalldUp    = "firewall-cmd --zone=public --add-interface=private-interface && firewall-cmd --add-port "
		firewalldDown  = "firewall-cmd --zone=public --add-interface=private-interface && firewall-cmd --remove-port "
		richRuleAdd    = "/udp && firewall-cmd --add-rich-rule='rule family=ipv4 source address=10.0.0.0/24 masquerade'"
		richRuleRemove = "/udp && firewall-cmd --remove-rich-rule='rule family=ipv6 source address=fd00::/64 masquerade'"
	)
	// The hooks may appear before or after ListenPort and may repeat.
	fixture := []struct {
		before, after string
		preview       *listenPortLineChange
	}{
		{before: " [INTERFACE] # section"},
		{before: "PrivateKey = server-secret"},
		{
			before:  "\tpostup  = \t" + iptablesUp + "0051820 -j ACCEPT # hook-comment-secret",
			after:   "\tpostup  = \t" + iptablesUp + "45555 -j ACCEPT # hook-comment-secret",
			preview: &listenPortLineChange{before: "PostUp = " + iptablesUp + "51820 -j ACCEPT", after: "PostUp = " + iptablesUp + "45555 -j ACCEPT"},
		},
		{
			before:  "lIsTeNpOrT = 0051820\t# port-comment-secret",
			after:   "lIsTeNpOrT = 45555\t# port-comment-secret",
			preview: &listenPortLineChange{before: "ListenPort = 51820", after: "ListenPort = 45555"},
		},
		{
			before:  "PostUp = " + iptablesUp + "51820 -j ACCEPT",
			after:   "PostUp = " + iptablesUp + "45555 -j ACCEPT",
			preview: &listenPortLineChange{before: "PostUp = " + iptablesUp + "51820 -j ACCEPT", after: "PostUp = " + iptablesUp + "45555 -j ACCEPT"},
		},
		{
			before:  "PostDown = " + iptablesDown + "51820 -j ACCEPT",
			after:   "PostDown = " + iptablesDown + "45555 -j ACCEPT",
			preview: &listenPortLineChange{before: "PostDown = " + iptablesDown + "51820 -j ACCEPT", after: "PostDown = " + iptablesDown + "45555 -j ACCEPT"},
		},
		{
			before:  "PostUp = " + ip6tablesUp + "51820 -j ACCEPT",
			after:   "PostUp = " + ip6tablesUp + "45555 -j ACCEPT",
			preview: &listenPortLineChange{before: "PostUp = " + ip6tablesUp + "51820 -j ACCEPT", after: "PostUp = " + ip6tablesUp + "45555 -j ACCEPT"},
		},
		{
			before:  "PostDown = " + ip6tablesDown + "51820 -j ACCEPT",
			after:   "PostDown = " + ip6tablesDown + "45555 -j ACCEPT",
			preview: &listenPortLineChange{before: "PostDown = " + ip6tablesDown + "51820 -j ACCEPT", after: "PostDown = " + ip6tablesDown + "45555 -j ACCEPT"},
		},
		{
			before:  ownedUp + " # hook-comment-secret",
			after:   strings.ReplaceAll(ownedUp, "51820", "45555") + " # hook-comment-secret",
			preview: &listenPortLineChange{before: ownedUp, after: strings.ReplaceAll(ownedUp, "51820", "45555")},
		},
		{
			before:  ownedDown,
			after:   strings.ReplaceAll(ownedDown, "51820", "45555"),
			preview: &listenPortLineChange{before: ownedDown, after: strings.ReplaceAll(ownedDown, "51820", "45555")},
		},
		{
			before:  "PostUp = " + firewalldUp + "51820" + richRuleAdd,
			after:   "PostUp = " + firewalldUp + "45555" + richRuleAdd,
			preview: &listenPortLineChange{before: "PostUp = firewall-cmd --add-port 51820/udp", after: "PostUp = firewall-cmd --add-port 45555/udp"},
		},
		{
			before:  "PostDown = " + firewalldDown + "51820" + richRuleRemove,
			after:   "PostDown = " + firewalldDown + "45555" + richRuleRemove,
			preview: &listenPortLineChange{before: "PostDown = firewall-cmd --remove-port 51820/udp", after: "PostDown = firewall-cmd --remove-port 45555/udp"},
		},
		{before: "PostUp = " + iptablesUp + "55555 -j ACCEPT"},
		{before: "PostUp = " + iptablesDown + "51820 -j ACCEPT"},
		{before: "PostUp = " + iptablesUp + "51820 -j ACCEPT && echo hook-secret"},
		{before: "PostUp = iptables -I INPUT -p tcp --dport 51820 -j ACCEPT"},
		{before: "PostUp = " + ip6tablesUp + "51820 -j ACCEPT && echo hook-secret"},
		{before: strings.Replace(ownedUp, "51820", "55555", 1)},
		{before: strings.Replace(ownedDown, "ip6tables", "iptables", 1)},
		{before: strings.ReplaceAll(ownedDown, "wgk-wg0", "custom-firewall")},
		{before: strings.Replace(ownedUp, "wgk-wg0", "wgk-wg1", 1)},
		{before: strings.ReplaceAll(ownedUp, "wgk-wg0", "wgk-wg0$(secret)")},
		{before: strings.ReplaceAll(ownedDown, "wgk-wg0", "wgk-..")},
		{before: ownedUp + " && echo hook-secret"},
		{before: "PostDown = " + firewalldDown + "51820" + richRuleAdd},
		{before: "[Peer]"},
		{before: "PostUp = " + iptablesUp + "51820 -j ACCEPT"},
		{before: "ListenPort = 51820"},
	}
	var beforeLines, afterLines []string
	var wantPreview []listenPortLineChange
	for i, line := range fixture {
		beforeLines = append(beforeLines, line.before)
		if line.preview == nil {
			afterLines = append(afterLines, line.before)
			continue
		}
		afterLines = append(afterLines, line.after)
		preview := *line.preview
		preview.lineNumber = i + 1
		wantPreview = append(wantPreview, preview)
	}
	before := strings.Join(beforeLines, "\r\n")
	after := strings.Join(afterLines, "\r\n")
	input := []byte(before)
	change, err := planListenPortChange(input, "45555")
	if err != nil {
		t.Fatal(err)
	}
	if change.beforePort != 51820 {
		t.Errorf("old port = %d, want 51820", change.beforePort)
	}
	if string(input) != before {
		t.Error("planning mutated the input")
	}
	if string(change.updated) != after {
		t.Errorf("updated configuration = %q, want %q", change.updated, after)
	}
	if !reflect.DeepEqual(change.lines, wantPreview) {
		t.Errorf("preview = %#v, want %#v", change.lines, wantPreview)
	}
}

func TestPlanListenPortChangeNoOp(t *testing.T) {
	const config = "[Interface]\nListenPort = 00051820 # comment\n"
	change, err := planListenPortChange([]byte(config), "51820")
	if err != nil {
		t.Fatal(err)
	}
	if change.beforePort != 51820 || len(change.lines) != 0 || string(change.updated) != config {
		t.Errorf("no-op plan = %#v", change)
	}
}

func TestPlanListenPortChangeRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name, config, port, wantError string
	}{
		{
			name:      "short port",
			config:    "[Interface]\nListenPort=51820",
			port:      "9999",
			wantError: errInvalidSetPort.Error(),
		},
		{
			name:      "out of range",
			config:    "[Interface]\nListenPort=51820",
			port:      "65536",
			wantError: errInvalidSetPort.Error(),
		},
		{
			name:      "non-ASCII input",
			config:    "[Interface]\nListenPort=51820",
			port:      "４５５５５",
			wantError: errInvalidSetPort.Error(),
		},
		{
			name:      "duplicate port",
			config:    "[Interface]\nListenPort=45454\nListenPort=23689",
			port:      "45555",
			wantError: "expected exactly one ListenPort in [Interface]",
		},
		{
			name:      "invalid port in config",
			config:    "[Interface]\nListenPort=private-value",
			port:      "45555",
			wantError: "ListenPort must be a decimal port from 1 to 65535",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := planListenPortChange([]byte(tt.config), tt.port); err == nil || err.Error() != tt.wantError {
				t.Fatalf("error = %v, want %q", err, tt.wantError)
			}
		})
	}
}
