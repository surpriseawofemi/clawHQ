//go:build !windows

package node

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Input injection is Windows-only for now; macOS needs CGEvent through CGO plus an
// Accessibility grant, and that deserves its own pass.
func inputSupported() bool { return false }

var errNoInput = fmt.Errorf("desktop control is not implemented on %s yet", runtime.GOOS)

func mouseMove(x, y int) error                      { return errNoInput }
func mouseButton(button string, down bool) error    { return errNoInput }
func mouseClick(button string, count int) error     { return errNoInput }
func mouseScroll(direction string, ticks int) error { return errNoInput }
func typeText(text string) error                    { return errNoInput }
func pressKeys(combo string) error                  { return errNoInput }
func holdKeys(combo string, d time.Duration) error  { return errNoInput }
func screenGeometry() (x, y, w, h int)              { return 0, 0, 0, 0 }

// showNotification uses Notification Center on macOS and is a no-op elsewhere.
func showNotification(title, body string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "default"`, esc(body), esc(title))
	return exec.Command("osascript", "-e", script).Run()
}

func holdKeysBegin(combo string) error { return errNoInput }
func holdKeysEnd(combo string)         {}
