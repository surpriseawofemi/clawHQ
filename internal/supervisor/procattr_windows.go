//go:build windows

package supervisor

import (
	"os/exec"
	"syscall"
)

// configureProcAttr hides the console window that would otherwise flash on every
// CLI invocation in a GUI app.
func configureProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
