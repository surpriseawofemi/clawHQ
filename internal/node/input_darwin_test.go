//go:build darwin && cgo

package node

import (
	"os"
	"testing"
	"time"
)

// TestDarwinInputSmoke drives the real screen and pointer, so it only runs on
// request: CLAWHQ_DARWIN_INPUT_TEST=1 go test ./internal/node -run DarwinInput -v
// It takes a screenshot and moves the pointer to the centre and back. No clicks, no
// typing. Expect permission prompts on a machine that has not granted them yet.
func TestDarwinInputSmoke(t *testing.T) {
	if os.Getenv("CLAWHQ_DARWIN_INPUT_TEST") != "1" {
		t.Skip("set CLAWHQ_DARWIN_INPUT_TEST=1 to run the live input smoke test")
	}

	t.Logf("accessibility trusted: %v", inputSupported())
	t.Logf("screen recording trusted: %v", screenRecordingTrusted())

	sx, sy, sw, sh := screenGeometry()
	t.Logf("screenGeometry: x=%d y=%d w=%d h=%d", sx, sy, sw, sh)
	if sw <= 0 || sh <= 0 {
		t.Fatalf("screenGeometry returned an empty display")
	}

	img, err := captureScreen(0)
	if err != nil {
		t.Errorf("captureScreen: %v", err)
	} else {
		t.Logf("captureScreen: %dx%d (scale %.2f)", img.Bounds().Dx(), img.Bounds().Dy(), float64(img.Bounds().Dx())/float64(sw))
	}

	startX, startY := cursorPos()
	t.Logf("pointer at %.0f,%.0f", startX, startY)
	if err := mouseMove(sx+sw/2, sy+sh/2); err != nil {
		t.Fatalf("mouseMove: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	mx, my := cursorPos()
	t.Logf("pointer after move to centre: %.0f,%.0f", mx, my)
	if err := mouseMove(int(startX), int(startY)); err != nil {
		t.Fatalf("mouseMove back: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	bx, by := cursorPos()
	t.Logf("pointer after move back: %.0f,%.0f", bx, by)
	if int(mx) != sx+sw/2 || int(my) != sy+sh/2 {
		t.Logf("pointer did not land on centre; Accessibility is probably not granted for this process")
	}
}
