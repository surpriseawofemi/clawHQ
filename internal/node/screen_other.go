//go:build !windows

package node

import (
	"fmt"
	"image"
)

// captureScreen is not implemented outside Windows yet. macOS needs ScreenCaptureKit
// (and a Screen Recording grant), which is a CGO job rather than a syscall shim.
func captureScreen(screenIndex int) (*image.RGBA, error) {
	return nil, fmt.Errorf("screen capture is not implemented on this platform yet")
}

func screenCaptureSupported() bool { return false }
