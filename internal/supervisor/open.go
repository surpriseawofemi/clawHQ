package supervisor

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenPath hands a file or folder to the OS handler, used for "Open log".
func OpenPath(path string) error {
	if path == "" {
		return fmt.Errorf("no path to open")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// The empty string is the window title that `start` expects first.
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	configureProcAttr(cmd)
	return cmd.Start()
}
