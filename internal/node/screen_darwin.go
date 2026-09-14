//go:build darwin

package node

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>

static int clawPreflightScreen(void) { return CGPreflightScreenCaptureAccess() ? 1 : 0; }
static int clawRequestScreen(void)   { return CGRequestScreenCaptureAccess() ? 1 : 0; }
*/
import "C"

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// macOS screen capture shells out to /usr/sbin/screencapture rather than binding
// ScreenCaptureKit: same Screen Recording grant, no Objective-C in the build.

const screenPermissionHint = "ClawHQ needs Screen Recording permission: System Settings → Privacy & Security → Screen Recording"

var requestScreenOnce sync.Once

// screenRecordingTrusted reports whether the process holds the Screen Recording grant.
func screenRecordingTrusted() bool { return C.clawPreflightScreen() == 1 }

// requestScreenRecording asks macOS to show the grant prompt. It is shown at most
// once per process, so a refusal does not nag on every screenshot.
func requestScreenRecording() {
	requestScreenOnce.Do(func() { C.clawRequestScreen() })
}

// captureScreen grabs one display as a PNG through screencapture and decodes it.
// screenIndex is zero-based; screencapture's -D is one-based and defaults to the
// main display when omitted. The image is Retina-sized on a Retina display.
func captureScreen(screenIndex int) (*image.RGBA, error) {
	if !screenRecordingTrusted() {
		requestScreenRecording()
	}

	f, err := os.CreateTemp(os.TempDir(), "clawhq-shot-*.png")
	if err != nil {
		return nil, err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	args := []string{"-x", "-t", "png"}
	if screenIndex > 0 {
		args = append(args, "-D", strconv.Itoa(screenIndex+1))
	}
	args = append(args, path)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "/usr/sbin/screencapture", args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("screencapture failed: %v %s (%s)", err, bytes.TrimSpace(stderr.Bytes()), screenPermissionHint)
	}

	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("screencapture produced no image (%s)", screenPermissionHint)
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %v", err)
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, fmt.Errorf("screencapture produced an empty image (%s)", screenPermissionHint)
	}

	img, ok := src.(*image.RGBA)
	if !ok {
		img = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	}
	if isBlack(img) {
		return nil, fmt.Errorf("screenshot came back black (%s)", screenPermissionHint)
	}
	return img, nil
}

// isBlack samples the image on a coarse grid; a capture without the grant can come
// back all black, and that is worth a clear message rather than a dark PNG.
func isBlack(img *image.RGBA) bool {
	b := img.Bounds()
	step := b.Dx() / 64
	if step < 1 {
		step = 1
	}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			i := img.PixOffset(x, y)
			if img.Pix[i] > 8 || img.Pix[i+1] > 8 || img.Pix[i+2] > 8 {
				return false
			}
		}
	}
	return true
}

func screenCaptureSupported() bool { return true }
