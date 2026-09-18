package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Clipboard commands, ClawHQ's own. They ride the same switch as desktop control:
// reading the clipboard can expose whatever the person last copied.
const (
	CmdClipboardGet = "clawhq.clipboard.get"
	CmdClipboardSet = "clawhq.clipboard.set"
	clipboardMax    = 256 * 1024
)

var errNoClipboard = errors.New("clipboard access is not implemented on this platform yet")

func clipboardRead() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/usr/bin/pbpaste")
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw")
	case "linux":
		if _, err := exec.LookPath("wl-paste"); err == nil {
			cmd = exec.CommandContext(ctx, "wl-paste", "--no-newline")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-o")
		}
	}
	if cmd == nil {
		return "", errNoClipboard
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("clipboard read failed: %w", err)
	}
	if len(out) > clipboardMax {
		out = out[:clipboardMax]
	}
	return string(out), nil
}

func clipboardWrite(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/usr/bin/pbcopy")
	case "windows":
		// Set-Clipboard reads stdin as lines; -Value from $input keeps newlines intact.
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "$t=[Console]::In.ReadToEnd(); Set-Clipboard -Value $t")
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.CommandContext(ctx, "wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard", "-i")
		}
	}
	if cmd == nil {
		return errNoClipboard
	}
	cmd.Stdin = bytes.NewBufferString(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clipboard write failed: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (h *Host) clipboardAllowed() string {
	h.mu.RLock()
	allowed := h.desktopControl
	h.mu.RUnlock()
	if !allowed {
		return "desktop control is switched off on this machine — the clipboard goes with it; enable it in ClawHQ → Settings → This machine"
	}
	return ""
}

// clipboardGet answers clawhq.clipboard.get with {text}.
func (h *Host) clipboardGet() (any, string) {
	if msg := h.clipboardAllowed(); msg != "" {
		return nil, msg
	}
	text, err := clipboardRead()
	if err != nil {
		return nil, err.Error()
	}
	return map[string]any{"text": text, "length": len(text)}, ""
}

// clipboardSet answers clawhq.clipboard.set {text}.
func (h *Host) clipboardSet(raw json.RawMessage) (any, string) {
	if msg := h.clipboardAllowed(); msg != "" {
		return nil, msg
	}
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "bad clipboard params: " + err.Error()
	}
	if len(p.Text) > clipboardMax {
		return nil, fmt.Sprintf("clipboard text is capped at %d bytes", clipboardMax)
	}
	if err := clipboardWrite(p.Text); err != nil {
		return nil, err.Error()
	}
	return map[string]any{"ok": true, "length": len(p.Text)}, ""
}

// ClipboardRead and ClipboardWrite expose the OS clipboard to the app itself (the
// terminal's right-click paste), not to agents.
func ClipboardRead() (string, error)   { return clipboardRead() }
func ClipboardWrite(text string) error { return clipboardWrite(text) }
