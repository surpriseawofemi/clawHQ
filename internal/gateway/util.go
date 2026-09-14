package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"runtime"
)

// platform reports the value the gateway expects in client.platform. It must be
// non-empty or connect is rejected at param validation.
func platform() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	default:
		return runtime.GOOS
	}
}

// newIdempotencyKey returns a unique key for chat.send, which requires one.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read does not fail in practice; a fixed prefix still beats panicking
		// and is unique enough per process for a retry-dedupe key.
		return "clawhq-fallback"
	}
	return hex.EncodeToString(b[:])
}
