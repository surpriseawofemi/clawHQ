//go:build !windows && !(darwin && cgo)

package node

import (
	"fmt"
	"runtime"
	"time"
)

// Input injection exists for Windows (SendInput) and macOS (CGEvent). Linux would
// need XTest or a Wayland portal, which deserves its own pass.
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

// showNotification is a no-op here; the in-app banner is the reliable path.
func showNotification(title, body string) error { return nil }

func holdKeysBegin(combo string) error { return errNoInput }
func holdKeysEnd(combo string)         {}
