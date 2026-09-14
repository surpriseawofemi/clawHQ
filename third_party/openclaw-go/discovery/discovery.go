// Package discovery provides local network discovery for OpenClaw Gateway
// instances using mDNS/DNS-SD (Bonjour).
//
// The gateway advertises itself as _openclaw-gw._tcp on the local network.
// This package browses for those services and returns structured beacon data.
//
// On macOS, discovery uses the native dns-sd CLI tool.
// On Linux, discovery uses avahi-browse.
//
// Reference: https://docs.openclaw.ai/gateway/discovery
package discovery

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ServiceType is the DNS-SD service type for OpenClaw Gateway.
const ServiceType = "_openclaw-gw._tcp"

// DefaultGatewayPort is the default gateway port when not specified in the beacon.
const DefaultGatewayPort = 18789

// Beacon represents a discovered OpenClaw Gateway on the local network.
type Beacon struct {
	// InstanceName is the mDNS instance name (e.g. "My Mac (OpenClaw)").
	InstanceName string

	// Domain is the DNS-SD domain (e.g. "local.").
	Domain string

	// Host is the resolved hostname from the SRV record.
	Host string

	// Port is the resolved port from the SRV record.
	Port int

	// DisplayName is the human-readable gateway name (from TXT).
	DisplayName string

	// LanHost is the <hostname>.local address (from TXT).
	LanHost string

	// TailnetDNS is the MagicDNS hostname if on a Tailscale network (from TXT).
	TailnetDNS string

	// GatewayPort is the gateway WebSocket/HTTP port (from TXT).
	GatewayPort int

	// GatewayTLS indicates whether TLS is enabled (from TXT).
	GatewayTLS bool

	// GatewayTLSFingerprint is the SHA-256 certificate fingerprint (from TXT).
	GatewayTLSFingerprint string

	// SSHPort is the SSH port on the gateway host (from TXT, full mode only).
	SSHPort int

	// CLIPath is the absolute path to the openclaw CLI binary (from TXT, full mode only).
	CLIPath string

	// Role is the beacon role (always "gateway" from TXT).
	Role string

	// Transport is the transport type (always "gateway" from TXT).
	Transport string

	// CanvasPort is the canvas host port if enabled (from TXT).
	CanvasPort int

	// TXT is the raw TXT record key-value map.
	TXT map[string]string
}

// WebSocketURL returns the WebSocket URL to connect to this gateway.
// It prefers the SRV-resolved host, then tailnetDns, then lanHost.
// Uses wss:// if GatewayTLS is true, otherwise ws://.
func (b *Beacon) WebSocketURL() string {
	host := b.pickHost()
	port := b.pickPort()
	scheme := "ws"
	if b.GatewayTLS {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// HTTPURL returns the HTTP base URL to connect to this gateway's REST endpoints.
func (b *Beacon) HTTPURL() string {
	host := b.pickHost()
	port := b.pickPort()
	scheme := "http"
	if b.GatewayTLS {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

func (b *Beacon) pickHost() string {
	if b.Host != "" {
		return b.Host
	}
	if b.TailnetDNS != "" {
		return b.TailnetDNS
	}
	if b.LanHost != "" {
		return b.LanHost
	}
	return "127.0.0.1"
}

func (b *Beacon) pickPort() int {
	if b.Port > 0 {
		return b.Port
	}
	if b.GatewayPort > 0 {
		return b.GatewayPort
	}
	return DefaultGatewayPort
}

// parseTXT populates the beacon's typed fields from the TXT map.
func (b *Beacon) parseTXT() {
	for k, v := range b.TXT {
		switch k {
		case "displayName":
			b.DisplayName = v
		case "lanHost":
			b.LanHost = v
		case "tailnetDns":
			b.TailnetDNS = v
		case "gatewayPort":
			if p, err := strconv.Atoi(v); err == nil {
				b.GatewayPort = p
			}
		case "gatewayTls":
			b.GatewayTLS = v == "1" || v == "true"
		case "gatewayTlsSha256":
			b.GatewayTLSFingerprint = v
		case "sshPort":
			if p, err := strconv.Atoi(v); err == nil {
				b.SSHPort = p
			}
		case "cliPath":
			b.CLIPath = v
		case "role":
			b.Role = v
		case "transport":
			b.Transport = v
		case "canvasPort":
			if p, err := strconv.Atoi(v); err == nil {
				b.CanvasPort = p
			}
		}
	}
}

// Browser discovers OpenClaw Gateways on the local network.
type Browser struct {
	// runCmd is an abstraction over exec.CommandContext for testing.
	// It takes (ctx, name, args...) and returns (stdout, error).
	runCmd func(ctx context.Context, name string, args ...string) (string, error)
}

// NewBrowser creates a new discovery browser.
func NewBrowser() *Browser {
	return &Browser{runCmd: defaultRunCmd}
}

// Browse discovers OpenClaw Gateway beacons on the local network.
// The ctx controls the timeout for discovery operations.
// Returns all beacons found within the timeout period.
func (b *Browser) Browse(ctx context.Context) ([]Beacon, error) {
	return b.browseOS(ctx)
}

// ---------------------------------------------------------------------------
// Parsing helpers (platform-independent)
// ---------------------------------------------------------------------------

// browseInstance is a parsed entry from a dns-sd browse.
type browseInstance struct {
	name   string
	domain string
}

// decodeDNSSDEscapes decodes DNS-SD decimal escape sequences like \032 (space)
// or multi-byte UTF-8 sequences like \226\128\153 (right single quotation mark).
// DNS-SD uses \DDD where DDD is a 3-digit decimal byte value (0-255).
func decodeDNSSDEscapes(s string) string {
	var result []byte
	i := 0
	for i < len(s) {
		if i+3 < len(s) && s[i] == '\\' && isDecDigit(s[i+1]) && isDecDigit(s[i+2]) && isDecDigit(s[i+3]) {
			val, _ := strconv.ParseUint(s[i+1:i+4], 10, 8)
			result = append(result, byte(val))
			i += 4
		} else {
			result = append(result, s[i])
			i++
		}
	}
	return string(result)
}

func isDecDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// parseTXTRecord parses a TXT record string like "key=value" into a key and value.
func parseTXTRecord(s string) (string, string) {
	s = strings.TrimSpace(s)
	// Remove surrounding quotes if present.
	s = strings.Trim(s, "\"")
	idx := strings.IndexByte(s, '=')
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx+1:]
}

// splitTXTFields splits a TXT line into individual key=value fields.
// Fields are separated by tabs or multiple spaces.
func splitTXTFields(line string) []string {
	// First try tab separation.
	if strings.Contains(line, "\t") {
		var fields []string
		for _, f := range strings.Split(line, "\t") {
			f = strings.TrimSpace(f)
			if f != "" && strings.Contains(f, "=") {
				fields = append(fields, f)
			}
		}
		if len(fields) > 0 {
			return fields
		}
	}
	// Fall back to splitting on whitespace.
	var fields []string
	for _, f := range strings.Fields(line) {
		f = strings.TrimSpace(f)
		if strings.Contains(f, "=") {
			fields = append(fields, f)
		}
	}
	return fields
}

// dedupeBeacons removes duplicate beacons by a composite key.
func dedupeBeacons(beacons []Beacon) []Beacon {
	seen := make(map[string]bool)
	var result []Beacon
	for _, b := range beacons {
		key := fmt.Sprintf("%s/%s/%s/%d/%d", b.Domain, b.InstanceName, b.Host, b.Port, b.GatewayPort)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, b)
	}
	return result
}

// parseBrowseOutput parses the output of macOS dns-sd -B.
func parseBrowseOutput(output string) []browseInstance {
	var instances []browseInstance
	seen := make(map[string]bool)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Browsing") || strings.HasPrefix(line, "DATE:") ||
			strings.HasPrefix(line, "Timestamp") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 7 {
			continue
		}

		action := parts[1]
		if action != "Add" {
			continue
		}

		domain := parts[4]
		regtypeIdx := -1
		for i, p := range parts {
			if strings.Contains(p, ServiceType) || strings.Contains(p, "_openclaw-gw") {
				regtypeIdx = i
				break
			}
		}
		if regtypeIdx < 0 || regtypeIdx+1 >= len(parts) {
			continue
		}

		name := decodeDNSSDEscapes(strings.Join(parts[regtypeIdx+1:], " "))
		key := domain + "/" + name
		if seen[key] {
			continue
		}
		seen[key] = true
		instances = append(instances, browseInstance{name: name, domain: domain})
	}
	return instances
}

// parseResolveOutput parses the output of macOS dns-sd -L.
func parseResolveOutput(output, instanceName, domain string) (*Beacon, error) {
	beacon := &Beacon{
		InstanceName: instanceName,
		Domain:       domain,
		TXT:          make(map[string]string),
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Look for "can be reached at <host>:<port>"
		if idx := strings.Index(line, "can be reached at"); idx >= 0 {
			after := strings.TrimSpace(line[idx+len("can be reached at"):])
			if paren := strings.Index(after, "("); paren > 0 {
				after = strings.TrimSpace(after[:paren])
			}
			if colonIdx := strings.LastIndex(after, ":"); colonIdx > 0 {
				beacon.Host = strings.TrimSuffix(after[:colonIdx], ".")
				if p, err := strconv.Atoi(after[colonIdx+1:]); err == nil {
					beacon.Port = p
				}
			}
			continue
		}

		// Look for TXT key=value pairs.
		if strings.Contains(line, "=") && !strings.HasPrefix(line, "Lookup") && !strings.HasPrefix(line, "DATE:") {
			for _, field := range splitTXTFields(line) {
				k, v := parseTXTRecord(field)
				if k != "" {
					beacon.TXT[k] = v
				}
			}
		}
	}

	beacon.parseTXT()

	if beacon.Host == "" && beacon.GatewayPort == 0 {
		return nil, fmt.Errorf("could not resolve instance %q", instanceName)
	}
	return beacon, nil
}

// parseAvahiBrowse parses the output of Linux avahi-browse -rpt.
func parseAvahiBrowse(output string) []Beacon {
	var beacons []Beacon
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "=") {
			continue
		}
		fields := strings.Split(line, ";")
		if len(fields) < 9 {
			continue
		}

		beacon := Beacon{
			InstanceName: fields[3],
			Domain:       fields[5],
			Host:         strings.TrimSuffix(fields[6], "."),
			TXT:          make(map[string]string),
		}
		if p, err := strconv.Atoi(fields[8]); err == nil {
			beacon.Port = p
		}

		if len(fields) > 9 {
			txtRaw := strings.Join(fields[9:], ";")
			for _, part := range strings.Fields(txtRaw) {
				k, v := parseTXTRecord(part)
				if k != "" {
					beacon.TXT[k] = v
				}
			}
		}

		beacon.parseTXT()
		beacons = append(beacons, beacon)
	}
	return dedupeBeacons(beacons)
}

// parseBeaconURL is a helper to parse a beacon's URL.
func parseBeaconURL(rawURL string) (*url.URL, error) {
	return url.Parse(rawURL)
}
