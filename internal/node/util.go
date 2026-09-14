package node

import "runtime"

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
