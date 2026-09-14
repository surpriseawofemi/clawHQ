// Package supervisor controls the OpenClaw gateway's lifecycle.
//
// It delegates to `openclaw daemon`, which already speaks the right service manager on
// each OS (schtasks on Windows, launchd on macOS, systemd on Linux). Spawning our own
// gateway child process would fight that service for the port and the gateway lock file.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Status struct {
	Installed    bool   `json:"installed"`
	Running      bool   `json:"running"`
	PID          int    `json:"pid"`
	UptimeMs     int64  `json:"uptimeMs"`
	CLIVersion   string `json:"cliVersion"`
	ServiceLabel string `json:"serviceLabel"`
	LogFile      string `json:"logFile"`
	Error        string `json:"error"`
}

// run invokes the openclaw CLI. On Windows `openclaw` is a shell shim, so it needs cmd
// to resolve rather than being exec'd directly.
func run(ctx context.Context, args ...string) ([]byte, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", append([]string{"/c", "openclaw"}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, "openclaw", args...)
	}
	configureProcAttr(cmd)
	return cmd.Output()
}

// Available reports whether the openclaw CLI is on PATH, which drives the onboarding copy.
func Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := run(ctx, "--version")
	return err == nil
}

func Get(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	out, err := run(ctx, "daemon", "status", "--json")
	if err != nil {
		return Status{Error: err.Error()}
	}

	var payload struct {
		CLI struct {
			Version string `json:"version"`
		} `json:"cli"`
		LogFile string `json:"logFile"`
		Service struct {
			Label   string `json:"label"`
			Loaded  bool   `json:"loaded"`
			Runtime struct {
				Status   string `json:"status"`
				PID      int    `json:"pid"`
				UptimeMs int64  `json:"uptimeMs"`
			} `json:"runtime"`
		} `json:"service"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Status{Error: fmt.Sprintf("could not parse daemon status: %v", err)}
	}

	return Status{
		Installed:    payload.Service.Loaded,
		Running:      payload.Service.Runtime.Status == "running",
		PID:          payload.Service.Runtime.PID,
		UptimeMs:     payload.Service.Runtime.UptimeMs,
		CLIVersion:   payload.CLI.Version,
		ServiceLabel: payload.Service.Label,
		LogFile:      payload.LogFile,
	}
}

// Control runs start, stop or restart and returns the resulting status.
func Control(ctx context.Context, action string) (Status, error) {
	switch action {
	case "start", "stop", "restart":
	default:
		return Status{}, fmt.Errorf("unsupported gateway action %q", action)
	}

	runCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()

	if _, err := run(runCtx, "daemon", action); err != nil {
		status := Get(ctx)
		status.Error = err.Error()
		return status, nil
	}
	return Get(ctx), nil
}

// MintSetupCode asks the local CLI for a pairing code, so onboarding is one click.
func MintSetupCode(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	out, err := run(ctx, "qr", "--json", "--no-ascii")
	if err != nil {
		return "", fmt.Errorf("openclaw qr: %w", err)
	}
	var payload struct {
		SetupCode string `json:"setupCode"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("could not parse setup code: %w", err)
	}
	if strings.TrimSpace(payload.SetupCode) == "" {
		return "", fmt.Errorf("`openclaw qr` returned no setup code")
	}
	return payload.SetupCode, nil
}
