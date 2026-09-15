package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// localGatewayToken reads the shared auth token of an OpenClaw gateway installed on
// this machine, from its config file. A gateway does not device-pair local clients;
// it wants this token, and the node role on the gateway host needs it once.
//
// The config is JSON5, so it is scanned rather than parsed: the first `"token": "…"`
// (or unquoted `token: "…"`) under gateway auth is the one.
func localGatewayToken() string {
	if t := strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN")); t != "" {
		return t
	}
	dir := strings.TrimSpace(os.Getenv("OPENCLAW_STATE_DIR"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".openclaw")
	}
	data, err := os.ReadFile(filepath.Join(dir, "openclaw.json"))
	if err != nil {
		return ""
	}
	text := string(data)
	// Narrow to the gateway.auth block when it can be found, so an unrelated
	// "token" key (a channel's bot token, say) is not mistaken for it.
	if i := strings.Index(text, "\"auth\""); i >= 0 {
		text = text[i:]
	} else if i := regexp.MustCompile(`\bauth\s*:`).FindStringIndex(text); i != nil {
		text = text[i[0]:]
	}
	m := regexp.MustCompile(`"?token"?\s*:\s*"([^"]+)"`).FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	log.Printf("node: using the local gateway's shared token from openclaw.json")
	return m[1]
}
