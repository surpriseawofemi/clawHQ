package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// --- Beacon ---

func TestBeaconWebSocketURL(t *testing.T) {
	tests := []struct {
		name   string
		beacon Beacon
		want   string
	}{
		{
			name:   "SRV host",
			beacon: Beacon{Host: "mymac.local", Port: 18789},
			want:   "ws://mymac.local:18789",
		},
		{
			name:   "TLS",
			beacon: Beacon{Host: "mymac.local", Port: 18789, GatewayTLS: true},
			want:   "wss://mymac.local:18789",
		},
		{
			name:   "fallback to tailnet",
			beacon: Beacon{TailnetDNS: "mymac.tailnet.ts.net", GatewayPort: 18789},
			want:   "ws://mymac.tailnet.ts.net:18789",
		},
		{
			name:   "fallback to lanHost",
			beacon: Beacon{LanHost: "mymac.local", GatewayPort: 18789},
			want:   "ws://mymac.local:18789",
		},
		{
			name:   "fallback to localhost and default port",
			beacon: Beacon{},
			want:   "ws://127.0.0.1:18789",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.beacon.WebSocketURL()
			if got != tt.want {
				t.Errorf("WebSocketURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBeaconHTTPURL(t *testing.T) {
	b := Beacon{Host: "mymac.local", Port: 18789}
	if got := b.HTTPURL(); got != "http://mymac.local:18789" {
		t.Errorf("HTTPURL() = %q", got)
	}

	b.GatewayTLS = true
	if got := b.HTTPURL(); got != "https://mymac.local:18789" {
		t.Errorf("HTTPURL() = %q", got)
	}
}

func TestBeaconPickHost(t *testing.T) {
	// Host priority: Host > TailnetDNS > LanHost > 127.0.0.1
	b := Beacon{Host: "h", TailnetDNS: "t", LanHost: "l"}
	if b.pickHost() != "h" {
		t.Errorf("pickHost = %q", b.pickHost())
	}

	b.Host = ""
	if b.pickHost() != "t" {
		t.Errorf("pickHost = %q", b.pickHost())
	}

	b.TailnetDNS = ""
	if b.pickHost() != "l" {
		t.Errorf("pickHost = %q", b.pickHost())
	}

	b.LanHost = ""
	if b.pickHost() != "127.0.0.1" {
		t.Errorf("pickHost = %q", b.pickHost())
	}
}

func TestBeaconPickPort(t *testing.T) {
	b := Beacon{Port: 1234, GatewayPort: 5678}
	if b.pickPort() != 1234 {
		t.Errorf("pickPort = %d", b.pickPort())
	}

	b.Port = 0
	if b.pickPort() != 5678 {
		t.Errorf("pickPort = %d", b.pickPort())
	}

	b.GatewayPort = 0
	if b.pickPort() != DefaultGatewayPort {
		t.Errorf("pickPort = %d", b.pickPort())
	}
}

func TestBeaconParseTXT(t *testing.T) {
	b := Beacon{
		TXT: map[string]string{
			"displayName":      "My Mac",
			"lanHost":          "mymac.local",
			"tailnetDns":       "mymac.tailnet.ts.net",
			"gatewayPort":      "18789",
			"gatewayTls":       "1",
			"gatewayTlsSha256": "abc123",
			"sshPort":          "22",
			"cliPath":          "/usr/local/bin/openclaw",
			"role":             "gateway",
			"transport":        "gateway",
			"canvasPort":       "18789",
		},
	}
	b.parseTXT()

	if b.DisplayName != "My Mac" {
		t.Errorf("DisplayName = %q", b.DisplayName)
	}
	if b.LanHost != "mymac.local" {
		t.Errorf("LanHost = %q", b.LanHost)
	}
	if b.TailnetDNS != "mymac.tailnet.ts.net" {
		t.Errorf("TailnetDNS = %q", b.TailnetDNS)
	}
	if b.GatewayPort != 18789 {
		t.Errorf("GatewayPort = %d", b.GatewayPort)
	}
	if !b.GatewayTLS {
		t.Error("GatewayTLS should be true")
	}
	if b.GatewayTLSFingerprint != "abc123" {
		t.Errorf("GatewayTLSFingerprint = %q", b.GatewayTLSFingerprint)
	}
	if b.SSHPort != 22 {
		t.Errorf("SSHPort = %d", b.SSHPort)
	}
	if b.CLIPath != "/usr/local/bin/openclaw" {
		t.Errorf("CLIPath = %q", b.CLIPath)
	}
	if b.Role != "gateway" {
		t.Errorf("Role = %q", b.Role)
	}
	if b.Transport != "gateway" {
		t.Errorf("Transport = %q", b.Transport)
	}
	if b.CanvasPort != 18789 {
		t.Errorf("CanvasPort = %d", b.CanvasPort)
	}
}

func TestBeaconParseTXTGatewayTlsTrue(t *testing.T) {
	b := Beacon{TXT: map[string]string{"gatewayTls": "true"}}
	b.parseTXT()
	if !b.GatewayTLS {
		t.Error("GatewayTLS should be true for 'true' string")
	}
}

func TestBeaconParseTXTInvalidPort(t *testing.T) {
	b := Beacon{TXT: map[string]string{
		"gatewayPort": "notanumber",
		"sshPort":     "notanumber",
		"canvasPort":  "notanumber",
	}}
	b.parseTXT()
	if b.GatewayPort != 0 || b.SSHPort != 0 || b.CanvasPort != 0 {
		t.Error("invalid port should remain 0")
	}
}

// --- Parsing helpers ---

func TestDecodeDNSSDEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{`My\032Mac`, "My Mac"},
		{`My\032Mac\032(OpenClaw)`, "My Mac (OpenClaw)"},
		// UTF-8 right single quotation mark: \226\128\153 → \xe2\x80\x99 → '
		{`Steve\226\128\153s`, "Steve\xe2\x80\x99s"},
		// No escape
		{"plain text", "plain text"},
		// Incomplete escape at end
		{`abc\03`, `abc\03`},
		// Value > 255 overflows uint8 (ParseUint returns max)
		{`abc\256`, "abc\xff"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := decodeDNSSDEscapes(tt.input)
			if got != tt.want {
				t.Errorf("decodeDNSSDEscapes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsDecDigit(t *testing.T) {
	for c := byte('0'); c <= '9'; c++ {
		if !isDecDigit(c) {
			t.Errorf("isDecDigit(%c) = false", c)
		}
	}
	for _, c := range []byte{'a', 'z', '/', ':'} {
		if isDecDigit(c) {
			t.Errorf("isDecDigit(%c) = true", c)
		}
	}
}

func TestParseTXTRecord(t *testing.T) {
	tests := []struct {
		input string
		wantK string
		wantV string
	}{
		{"key=value", "key", "value"},
		{"key=", "key", ""},
		{"key", "key", ""},
		{`"key=value"`, "key", "value"},
		{" key=value ", "key", "value"},
		{"key=val=ue", "key", "val=ue"},
	}
	for _, tt := range tests {
		k, v := parseTXTRecord(tt.input)
		if k != tt.wantK || v != tt.wantV {
			t.Errorf("parseTXTRecord(%q) = (%q, %q), want (%q, %q)", tt.input, k, v, tt.wantK, tt.wantV)
		}
	}
}

func TestSplitTXTFields(t *testing.T) {
	// Tab-separated
	line := "displayName=My Mac\trole=gateway\ttransport=gateway"
	fields := splitTXTFields(line)
	if len(fields) != 3 {
		t.Fatalf("splitTXTFields (tab) = %d, want 3", len(fields))
	}

	// Space-separated
	line = "displayName=My role=gateway transport=gateway"
	fields = splitTXTFields(line)
	if len(fields) != 3 {
		t.Fatalf("splitTXTFields (space) = %d, want 3: %v", len(fields), fields)
	}

	// Tab-separated with non-kv entries
	line = "some text\tkey=val\t\t"
	fields = splitTXTFields(line)
	if len(fields) != 1 || fields[0] != "key=val" {
		t.Errorf("splitTXTFields = %v", fields)
	}
}

func TestDedupeBeacons(t *testing.T) {
	beacons := []Beacon{
		{Domain: "local.", InstanceName: "Mac", Host: "mac.local", Port: 18789},
		{Domain: "local.", InstanceName: "Mac", Host: "mac.local", Port: 18789}, // duplicate
		{Domain: "local.", InstanceName: "Linux", Host: "linux.local", Port: 18789},
	}
	got := dedupeBeacons(beacons)
	if len(got) != 2 {
		t.Errorf("dedupeBeacons = %d, want 2", len(got))
	}
}

// --- parseBrowseOutput ---

func TestParseBrowseOutput(t *testing.T) {
	output := `Browsing for _openclaw-gw._tcp
DATE: ---Fri 01 Jan 2025---
 3:00:00.000  Add        2  4 local.       _openclaw-gw._tcp.     My Mac (OpenClaw)
 3:00:00.000  Add        2  4 local.       _openclaw-gw._tcp.     My\032Mac\032Two
 3:00:00.000  Rmv        2  4 local.       _openclaw-gw._tcp.     Old Mac
`
	instances := parseBrowseOutput(output)
	if len(instances) != 2 {
		t.Fatalf("parseBrowseOutput = %d, want 2: %v", len(instances), instances)
	}
	if instances[0].name != "My Mac (OpenClaw)" {
		t.Errorf("instance[0].name = %q", instances[0].name)
	}
	if instances[1].name != "My Mac Two" {
		t.Errorf("instance[1].name = %q", instances[1].name)
	}
}

func TestParseBrowseOutputEmpty(t *testing.T) {
	instances := parseBrowseOutput("")
	if len(instances) != 0 {
		t.Errorf("parseBrowseOutput empty = %d", len(instances))
	}
}

func TestParseBrowseOutputDedupe(t *testing.T) {
	output := ` 3:00:00.000  Add        2  4 local.       _openclaw-gw._tcp.     Mac
 3:00:00.000  Add        2  4 local.       _openclaw-gw._tcp.     Mac
`
	instances := parseBrowseOutput(output)
	if len(instances) != 1 {
		t.Errorf("expected deduped, got %d", len(instances))
	}
}

func TestParseBrowseOutputShortLine(t *testing.T) {
	output := "short line"
	instances := parseBrowseOutput(output)
	if len(instances) != 0 {
		t.Errorf("short line = %d", len(instances))
	}
}

func TestParseBrowseOutputNoRegtype(t *testing.T) {
	output := ` 3:00:00.000  Add  2  4 local. _other._tcp. Instance`
	instances := parseBrowseOutput(output)
	if len(instances) != 0 {
		t.Errorf("no regtype match = %d", len(instances))
	}
}

// --- parseResolveOutput ---

func TestParseResolveOutput(t *testing.T) {
	output := `Lookup My Mac (OpenClaw)._openclaw-gw._tcp.local.
DATE: ---Fri 01 Jan 2025---
 3:00:00.000  My\032Mac\032(OpenClaw)._openclaw-gw._tcp.local. can be reached at mymac.local.:18789 (interface 4)
 displayName=My Mac	role=gateway	transport=gateway	gatewayPort=18789	lanHost=mymac.local
`
	beacon, err := parseResolveOutput(output, "My Mac (OpenClaw)", "local.")
	if err != nil {
		t.Fatalf("parseResolveOutput: %v", err)
	}
	if beacon.Host != "mymac.local" {
		t.Errorf("Host = %q", beacon.Host)
	}
	if beacon.Port != 18789 {
		t.Errorf("Port = %d", beacon.Port)
	}
	if beacon.DisplayName != "My Mac" {
		t.Errorf("DisplayName = %q", beacon.DisplayName)
	}
	if beacon.Role != "gateway" {
		t.Errorf("Role = %q", beacon.Role)
	}
	if beacon.GatewayPort != 18789 {
		t.Errorf("GatewayPort = %d", beacon.GatewayPort)
	}
}

func TestParseResolveOutputEmpty(t *testing.T) {
	_, err := parseResolveOutput("", "test", "local.")
	if err == nil {
		t.Error("expected error for empty output")
	}
}

func TestParseResolveOutputNoPort(t *testing.T) {
	// Has "can be reached at" but no valid port
	output := ` 3:00 test._openclaw-gw._tcp.local. can be reached at host (interface 4)`
	_, err := parseResolveOutput(output, "test", "local.")
	if err == nil {
		t.Error("expected error for no host:port")
	}
}

// --- parseAvahiBrowse ---

func TestParseAvahiBrowse(t *testing.T) {
	output := `+;eth0;IPv4;My Linux;_openclaw-gw._tcp;local;
=;eth0;IPv4;My Linux;_openclaw-gw._tcp;local;mylinux.local;192.168.1.10;18789;"displayName=MyLinux" "role=gateway" "transport=gateway" "gatewayPort=18789"
`
	beacons := parseAvahiBrowse(output)
	if len(beacons) != 1 {
		t.Fatalf("parseAvahiBrowse = %d, want 1", len(beacons))
	}
	b := beacons[0]
	if b.InstanceName != "My Linux" {
		t.Errorf("InstanceName = %q", b.InstanceName)
	}
	if b.Host != "mylinux.local" {
		t.Errorf("Host = %q", b.Host)
	}
	if b.Port != 18789 {
		t.Errorf("Port = %d", b.Port)
	}
	if b.DisplayName != "MyLinux" {
		t.Errorf("DisplayName = %q", b.DisplayName)
	}
	if b.Role != "gateway" {
		t.Errorf("Role = %q", b.Role)
	}
	if b.GatewayPort != 18789 {
		t.Errorf("GatewayPort = %d", b.GatewayPort)
	}
}

func TestParseAvahiBrowseEmpty(t *testing.T) {
	beacons := parseAvahiBrowse("")
	if len(beacons) != 0 {
		t.Errorf("empty = %d", len(beacons))
	}
}

func TestParseAvahiBrowseShortFields(t *testing.T) {
	output := "=;eth0;IPv4;short"
	beacons := parseAvahiBrowse(output)
	if len(beacons) != 0 {
		t.Errorf("short = %d", len(beacons))
	}
}

func TestParseAvahiBrowseNoTXT(t *testing.T) {
	output := "=;eth0;IPv4;No TXT;_openclaw-gw._tcp;local;host.local;192.168.1.10;18789"
	beacons := parseAvahiBrowse(output)
	if len(beacons) != 1 {
		t.Fatalf("no txt = %d", len(beacons))
	}
	if beacons[0].Port != 18789 {
		t.Errorf("Port = %d", beacons[0].Port)
	}
}

// --- Browser with mock ---

func TestBrowseWithMock(t *testing.T) {
	browser := &Browser{
		runCmd: func(ctx context.Context, name string, args ...string) (string, error) {
			// macOS: dns-sd -B / dns-sd -L
			if name == "dns-sd" && args[0] == "-B" {
				return ` 3:00:00.000  Add  2  4 local. _openclaw-gw._tcp. TestGateway`, nil
			}
			if name == "dns-sd" && args[0] == "-L" {
				return ` 3:00:00.000  TestGateway._openclaw-gw._tcp.local. can be reached at testhost.local.:18789 (interface 4)
 displayName=TestGW	role=gateway	gatewayPort=18789`, nil
			}
			// Linux: avahi-browse -rpt
			if name == "avahi-browse" {
				return `=;eth0;IPv4;TestGateway;_openclaw-gw._tcp;local;testhost.local;192.168.1.10;18789;"displayName=TestGW" "role=gateway" "gatewayPort=18789"`, nil
			}
			return "", fmt.Errorf("unexpected cmd: %s %v", name, args)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	beacons, err := browser.Browse(ctx)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(beacons) != 1 {
		t.Fatalf("beacons = %d, want 1", len(beacons))
	}
	if beacons[0].Host != "testhost.local" {
		t.Errorf("Host = %q", beacons[0].Host)
	}
	if beacons[0].DisplayName != "TestGW" {
		t.Errorf("DisplayName = %q", beacons[0].DisplayName)
	}
}

func TestBrowseEmptyResult(t *testing.T) {
	browser := &Browser{
		runCmd: func(ctx context.Context, name string, args ...string) (string, error) {
			return "", nil
		},
	}

	ctx := context.Background()
	beacons, err := browser.Browse(ctx)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(beacons) != 0 {
		t.Errorf("beacons = %d, want 0", len(beacons))
	}
}

func TestBrowseResolveError(t *testing.T) {
	browser := &Browser{
		runCmd: func(ctx context.Context, name string, args ...string) (string, error) {
			// macOS: dns-sd -B returns an instance, but -L resolve fails
			if name == "dns-sd" && args[0] == "-B" {
				return ` 3:00:00.000  Add  2  4 local. _openclaw-gw._tcp. TestGateway`, nil
			}
			// Linux: avahi-browse returns nothing useful
			if name == "avahi-browse" {
				return "", nil
			}
			// Resolve returns nothing useful.
			return "", nil
		},
	}

	ctx := context.Background()
	beacons, err := browser.Browse(ctx)
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(beacons) != 0 {
		t.Errorf("beacons = %d, want 0 (resolve failed)", len(beacons))
	}
}

// --- NewBrowser ---

func TestNewBrowser(t *testing.T) {
	b := NewBrowser()
	if b == nil {
		t.Fatal("NewBrowser returned nil")
	}
	if b.runCmd == nil {
		t.Fatal("runCmd is nil")
	}
}

// --- Constants ---

func TestConstants(t *testing.T) {
	if ServiceType != "_openclaw-gw._tcp" {
		t.Errorf("ServiceType = %q", ServiceType)
	}
	if DefaultGatewayPort != 18789 {
		t.Errorf("DefaultGatewayPort = %d", DefaultGatewayPort)
	}
}

// --- parseBeaconURL ---

func TestParseBeaconURL(t *testing.T) {
	u, err := parseBeaconURL("ws://mymac.local:18789")
	if err != nil {
		t.Fatalf("parseBeaconURL: %v", err)
	}
	if u.Scheme != "ws" || u.Host != "mymac.local:18789" {
		t.Errorf("parsed = %+v", u)
	}
}
