//go:build !windows

package supervisor

import "os/exec"

// configureProcAttr is a no-op outside Windows; there is no console to hide.
func configureProcAttr(cmd *exec.Cmd) {}
