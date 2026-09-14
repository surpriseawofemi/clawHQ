//go:build !windows && !darwin

package node

import (
	"fmt"
	"image"
)

// captureScreen is implemented on Windows (GDI) and macOS (screencapture). Linux
// needs X11 or a portal request and is not done yet.
func captureScreen(screenIndex int) (*image.RGBA, error) {
	return nil, fmt.Errorf("screen capture is not implemented on this platform yet")
}

func screenCaptureSupported() bool { return false }
