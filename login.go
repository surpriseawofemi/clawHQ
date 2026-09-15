package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// LoginStatus says whether ClawHQ starts with the user's session.
type LoginStatus struct {
	Enabled   bool `json:"enabled"`
	Supported bool `json:"supported"`
	// Path is the launch agent (macOS) or registry key (Windows) that does it.
	Path string `json:"path"`
}

// LoginService registers ClawHQ as a login item. macOS gets a launch agent that
// runs `open -a` on the bundle, so an update that replaces the app still launches;
// Windows gets a Run key. Both are files or keys the user can inspect and remove.
type LoginService struct{}

const launchAgentLabel = "com.clawhq.app"

func launchAgentPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// bundlePath is the .app this binary runs from, or the binary itself outside one.
func bundlePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		return exe[:i+4]
	}
	return exe
}

func (s *LoginService) Status() LoginStatus {
	switch runtime.GOOS {
	case "darwin":
		p := launchAgentPath()
		_, err := os.Stat(p)
		return LoginStatus{Enabled: err == nil, Supported: true, Path: p}
	case "windows":
		out, err := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ClawHQ").Output()
		return LoginStatus{Enabled: err == nil && strings.Contains(string(out), "ClawHQ"), Supported: true, Path: `HKCU\...\Run\ClawHQ`}
	default:
		return LoginStatus{}
	}
}

// Set turns the login item on or off.
func (s *LoginService) Set(on bool) (LoginStatus, error) {
	switch runtime.GOOS {
	case "darwin":
		p := launchAgentPath()
		if p == "" {
			return s.Status(), errors.New("no home directory")
		}
		if !on {
			_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), launchAgentLabel)).Run()
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return s.Status(), err
			}
			return s.Status(), nil
		}
		target := bundlePath()
		if target == "" {
			return s.Status(), errors.New("cannot tell where ClawHQ is installed")
		}
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/bin/open</string>
		<string>-a</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, launchAgentLabel, target)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return s.Status(), err
		}
		if err := os.WriteFile(p, []byte(plist), 0o644); err != nil {
			return s.Status(), err
		}
		return s.Status(), nil
	case "windows":
		if !on {
			_ = exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ClawHQ", "/f").Run()
			return s.Status(), nil
		}
		exe, err := os.Executable()
		if err != nil {
			return s.Status(), err
		}
		if err := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "ClawHQ", "/t", "REG_SZ", "/d", `"`+exe+`"`, "/f").Run(); err != nil {
			return s.Status(), fmt.Errorf("registry write failed: %w", err)
		}
		return s.Status(), nil
	default:
		return s.Status(), errors.New("launch at login is not wired up on this platform yet")
	}
}
