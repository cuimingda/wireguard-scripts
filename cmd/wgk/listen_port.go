package main

import (
	"bytes"
	"errors"
	"strconv"
)

func parseListenPort(data []byte) (int, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return 0, errors.New("configuration contains a NUL byte")
	}

	const whitespace = " \t\n\r\v\f"
	var rawPort []byte
	inInterface := false
	portFields := 0
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		line, _, _ = bytes.Cut(line, []byte("#"))
		key, value, _ := bytes.Cut(line, []byte("="))
		key = bytes.Trim(key, whitespace)
		if bytes.HasPrefix(key, []byte("[")) {
			inInterface = equalASCIIFold(key, "[interface]")
		} else if inInterface && equalASCIIFold(key, "listenport") {
			rawPort = bytes.Trim(value, whitespace)
			portFields++
		}
	}
	if portFields != 1 {
		return 0, errors.New("expected exactly one ListenPort in [Interface]")
	}

	port, err := strconv.ParseUint(string(rawPort), 10, 16)
	if err != nil || port == 0 {
		return 0, errors.New("ListenPort must be a decimal port from 1 to 65535")
	}
	return int(port), nil
}

func equalASCIIFold(value []byte, ascii string) bool {
	// Any non-ASCII rune equal under Unicode folding takes more than one byte.
	return len(value) == len(ascii) && bytes.EqualFold(value, []byte(ascii))
}
