//go:build windows

package discovery

import (
	"context"
	"fmt"
	"runtime"
)

// defaultRunCmd is a no-op on Windows since mDNS discovery is not supported.
func defaultRunCmd(_ context.Context, _ string, _ ...string) (string, error) {
	return "", fmt.Errorf("mDNS discovery is not supported on %s", runtime.GOOS)
}

// browseOS is not supported on Windows.
func (b *Browser) browseOS(_ context.Context) ([]Beacon, error) {
	return nil, fmt.Errorf("mDNS discovery is not supported on %s", runtime.GOOS)
}
