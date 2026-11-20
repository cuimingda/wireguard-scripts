package main

import (
	"bytes"
	"errors"
	"iter"
	"regexp"
	"strconv"
)

const wireGuardWhitespace = " \t\n\r\v\f"

type wireGuardField struct {
	key, value  []byte
	valueOffset int
	lineNumber  int
}

type listenPortChange struct {
	beforePort int
	lines      []listenPortLineChange
	updated    []byte
}

type listenPortLineChange struct {
	lineNumber    int
	before, after string
}

type listenPortEdit struct {
	start, end                   int
	prefix, betweenPorts, suffix string
}

func (edit listenPortEdit) replacement(port string) string {
	if edit.betweenPorts != "" {
		return port + edit.betweenPorts + port
	}
	return port
}

const ownedPortRule = ` INPUT -p udp --dport ([0-9]+) -j ACCEPT -m comment --comment ` + installFirewallOwner

var (
	iptablesPortHook = regexp.MustCompile(
		`^[[:space:]]*(ip6?tables)[[:space:]]+(-[ID])[[:space:]]+INPUT[[:space:]]+-p[[:space:]]+udp[[:space:]]+` +
			`--dport[[:space:]]+([0-9]+)[[:space:]]+-j[[:space:]]+ACCEPT[[:space:]]*$`,
	)
	ownedPortUpHook = regexp.MustCompile(`^[[:space:]]*(ip6?tables) -C` + ownedPortRule +
		` \|\| (ip6?tables) -I` + ownedPortRule + `[[:space:]]*$`)
	ownedPortDownHook = regexp.MustCompile(`^[[:space:]]*if (ip6?tables) -C` + ownedPortRule +
		`; then (ip6?tables) -D` + ownedPortRule + `; fi[[:space:]]*$`)
	firewalldPortHook = regexp.MustCompile(
		`^[[:space:]]*firewall-cmd[[:space:]]+--zone=public[[:space:]]+--add-interface=[A-Za-z0-9_=+.-]+` +
			`[[:space:]]*&&[[:space:]]*firewall-cmd[[:space:]]+--(add|remove)-port[[:space:]]+([0-9]+)/udp` +
			`[[:space:]]*&&[[:space:]]*firewall-cmd[[:space:]]+--(add|remove)-rich-rule=`,
	)
)

func parseListenPort(data []byte) (int, error) {
	port, _, err := findListenPort(data)
	return port, err
}

func findListenPort(data []byte) (int, wireGuardField, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return 0, wireGuardField{}, errors.New("configuration contains a NUL byte")
	}

	var portField wireGuardField
	portFields := 0
	for field := range wireGuardInterfaceFields(data) {
		if equalASCIIFold(field.key, "listenport") {
			portField = field
			portFields++
		}
	}
	if portFields != 1 {
		return 0, wireGuardField{}, errors.New("expected exactly one ListenPort in [Interface]")
	}

	port := decimalListenPort(bytes.Trim(portField.value, wireGuardWhitespace))
	if port == 0 {
		return 0, wireGuardField{}, errors.New("ListenPort must be a decimal port from 1 to 65535")
	}
	return port, portField, nil
}

func wireGuardInterfaceFields(data []byte) iter.Seq[wireGuardField] {
	return func(yield func(wireGuardField) bool) {
		inInterface := false
		lineNumber, offset := 0, 0
		for line := range bytes.SplitSeq(data, []byte("\n")) {
			lineNumber++
			start := offset
			offset += len(line) + 1
			setting, _, _ := bytes.Cut(line, []byte("#"))
			rawKey, value, _ := bytes.Cut(setting, []byte("="))
			key := bytes.Trim(rawKey, wireGuardWhitespace)
			if bytes.HasPrefix(key, []byte("[")) {
				inInterface = equalASCIIFold(key, "[interface]")
			} else if inInterface {
				if !yield(wireGuardField{
					key:         key,
					value:       value,
					valueOffset: start + len(rawKey) + 1,
					lineNumber:  lineNumber,
				}) {
					return
				}
			}
		}
	}
}

func planListenPortChange(data []byte, newPort string) (listenPortChange, error) {
	if !validMaintenancePort(newPort) {
		return listenPortChange{}, errInvalidSetPort
	}
	oldPort, portField, err := findListenPort(data)
	if err != nil {
		return listenPortChange{}, err
	}
	change := listenPortChange{beforePort: oldPort, updated: data}
	oldText := strconv.Itoa(oldPort)
	if newPort == oldText {
		return change, nil
	}

	updated := make([]byte, 0, len(data))
	previousEnd := 0
	for field := range wireGuardInterfaceFields(data) {
		var edit listenPortEdit
		if field.lineNumber == portField.lineNumber {
			value := bytes.TrimLeft(field.value, wireGuardWhitespace)
			start := len(field.value) - len(value)
			end := start + len(bytes.TrimRight(value, wireGuardWhitespace))
			edit = listenPortEdit{start: start, end: end, prefix: "ListenPort = "}
		} else if hookEdit, ok := listenPortHookEdit(field, oldPort); ok {
			edit = hookEdit
		} else {
			continue
		}

		start, end := field.valueOffset+edit.start, field.valueOffset+edit.end
		updated = append(updated, data[previousEnd:start]...)
		replacement := edit.replacement(newPort)
		updated = append(updated, replacement...)
		previousEnd = end
		// Render only known syntax and port numbers. Hooks and comments can contain secrets.
		change.lines = append(change.lines, listenPortLineChange{
			lineNumber: field.lineNumber,
			before:     edit.prefix + edit.replacement(oldText) + edit.suffix,
			after:      edit.prefix + replacement + edit.suffix,
		})
	}
	change.updated = append(updated, data[previousEnd:]...)
	return change, nil
}

func listenPortHookEdit(field wireGuardField, oldPort int) (listenPortEdit, bool) {
	var label, iptablesAction, firewalldAction string
	var ownedPortHook *regexp.Regexp
	switch {
	case equalASCIIFold(field.key, "postup"):
		label, iptablesAction, firewalldAction = "PostUp", "-I", "add"
		ownedPortHook = ownedPortUpHook
	case equalASCIIFold(field.key, "postdown"):
		label, iptablesAction, firewalldAction = "PostDown", "-D", "remove"
		ownedPortHook = ownedPortDownHook
	default:
		return listenPortEdit{}, false
	}

	value := field.value
	if match := ownedPortHook.FindSubmatchIndex(value); match != nil &&
		bytes.Equal(value[match[2]:match[3]], value[match[6]:match[7]]) &&
		decimalListenPort(value[match[4]:match[5]]) == oldPort && decimalListenPort(value[match[8]:match[9]]) == oldPort {
		return listenPortEdit{
			start:        match[4],
			end:          match[9],
			prefix:       label + " = " + string(bytes.TrimLeft(value[:match[4]], wireGuardWhitespace)),
			betweenPorts: string(value[match[5]:match[8]]),
			suffix:       string(bytes.TrimRight(value[match[9]:], wireGuardWhitespace)),
		}, true
	}
	if match := iptablesPortHook.FindSubmatchIndex(value); match != nil &&
		bytes.Equal(value[match[4]:match[5]], []byte(iptablesAction)) && decimalListenPort(value[match[6]:match[7]]) == oldPort {
		executable := string(value[match[2]:match[3]])
		return listenPortEdit{
			start:  match[6],
			end:    match[7],
			prefix: label + " = " + executable + " " + iptablesAction + " INPUT -p udp --dport ",
			suffix: " -j ACCEPT",
		}, true
	}
	if match := firewalldPortHook.FindSubmatchIndex(value); match != nil &&
		bytes.Equal(value[match[2]:match[3]], []byte(firewalldAction)) && decimalListenPort(value[match[4]:match[5]]) == oldPort &&
		bytes.Equal(value[match[6]:match[7]], []byte(firewalldAction)) {
		return listenPortEdit{
			start:  match[4],
			end:    match[5],
			prefix: label + " = firewall-cmd --" + firewalldAction + "-port ",
			suffix: "/udp",
		}, true
	}
	return listenPortEdit{}, false
}

func decimalListenPort(value []byte) int {
	port, err := strconv.ParseUint(string(value), 10, 16)
	if err != nil {
		return 0
	}
	return int(port)
}

func equalASCIIFold(value []byte, ascii string) bool {
	// Any non-ASCII rune equal under Unicode folding takes more than one byte.
	return len(value) == len(ascii) && bytes.EqualFold(value, []byte(ascii))
}
