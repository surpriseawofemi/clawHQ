//go:build linux

package discovery

import (
	"context"
	"os/exec"
	"time"
)

// defaultRunCmd executes a command and returns its stdout.
func defaultRunCmd(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return string(out), err
}

// browseOS uses avahi-browse on Linux to discover OpenClaw gateways.
func (b *Browser) browseOS(ctx context.Context) ([]Beacon, error) {
	browseCtx, browseCancel := context.WithTimeout(ctx, 2*time.Second)
	defer browseCancel()

	browseOut, _ := b.runCmd(browseCtx, "avahi-browse", "-rpt", ServiceType)
	return parseAvahiBrowse(browseOut), nil
}
