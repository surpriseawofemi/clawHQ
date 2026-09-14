//go:build darwin

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

// browseOS uses macOS dns-sd to discover OpenClaw gateways.
func (b *Browser) browseOS(ctx context.Context) ([]Beacon, error) {
	browseCtx, browseCancel := context.WithTimeout(ctx, 2*time.Second)
	defer browseCancel()

	browseOut, _ := b.runCmd(browseCtx, "dns-sd", "-B", ServiceType, "local.")
	instances := parseBrowseOutput(browseOut)

	var beacons []Beacon
	for _, inst := range instances {
		beacon, err := b.resolveInstance(ctx, inst.name, inst.domain)
		if err != nil {
			continue
		}
		beacons = append(beacons, *beacon)
	}
	return dedupeBeacons(beacons), nil
}

// resolveInstance uses dns-sd -L to resolve a service instance.
func (b *Browser) resolveInstance(ctx context.Context, name, domain string) (*Beacon, error) {
	resolveCtx, resolveCancel := context.WithTimeout(ctx, 2*time.Second)
	defer resolveCancel()

	resolveOut, _ := b.runCmd(resolveCtx, "dns-sd", "-L", name, ServiceType, domain)
	return parseResolveOutput(resolveOut, name, domain)
}
