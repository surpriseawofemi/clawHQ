package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/sshx"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"
)

// ServerService: machines reached over SSH, independent of OpenClaw. Health
// checks, Claude Code install, and interactive shells streamed to the page.
type ServerService struct {
	store  *store.Store
	app    *application.App
	mu     sync.Mutex
	shells map[string]*openShell
	cmds   map[string]*openCommand
	seq    int
}

type openShell struct {
	client *ssh.Client
	shell  *sshx.Shell
}

type openCommand struct {
	client *ssh.Client
	cmd    *sshx.Command
}

// ServerView is a profile without its secret.
type ServerView struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Host        string               `json:"host"`
	Port        int                  `json:"port"`
	User        string               `json:"user"`
	Auth        string               `json:"auth"`
	KeyPath     string               `json:"keyPath,omitempty"`
	HasPassword bool                 `json:"hasPassword"`
	Dir         string               `json:"dir,omitempty"`
	AddedAtMs   int64                `json:"addedAtMs"`
	LastOkAtMs  int64                `json:"lastOkAtMs,omitempty"`
	Actions     []store.ServerAction `json:"actions"`
}

// ServerInput is what the page sends to save a server.
type ServerInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Auth     string `json:"auth"`
	KeyPath  string `json:"keyPath"`
	Password string `json:"password"`
	Dir      string `json:"dir"`
}

type ServerCheck struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	OK    bool   `json:"ok"`
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
}

type ServerTool struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

type ClaudeStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	LoggedIn  bool   `json:"loggedIn"`
	Account   string `json:"account"`
}

type ServerHealth struct {
	OK          bool          `json:"ok"`
	Error       string        `json:"error,omitempty"`
	CheckedAtMs int64         `json:"checkedAtMs"`
	Hostname    string        `json:"hostname"`
	OS          string        `json:"os"`
	User        string        `json:"user"`
	Uptime      string        `json:"uptime"`
	Load        string        `json:"load"`
	Disk        string        `json:"disk"`
	Memory      string        `json:"memory"`
	Home        string        `json:"home"`
	Node        ServerTool    `json:"node"`
	Git         ServerTool    `json:"git"`
	Claude      ClaudeStatus  `json:"claude"`
	Checks      []ServerCheck `json:"checks"`
}

func knownHostsPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, "ClawHQ", "known_hosts")
}

func viewOf(p store.ServerProfile) ServerView {
	actions := p.Actions
	if actions == nil {
		actions = []store.ServerAction{}
	}
	return ServerView{ID: p.ID, Name: p.Name, Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, HasPassword: p.Password != "", Dir: p.Dir, AddedAtMs: p.AddedAtMs, LastOkAtMs: p.LastOkAtMs, Actions: actions}
}

func (s *ServerService) List() []ServerView {
	cfg := s.store.Read()
	out := make([]ServerView, 0, len(cfg.Servers))
	for _, p := range cfg.Servers {
		out = append(out, viewOf(p))
	}
	return out
}

func (s *ServerService) Save(in ServerInput) ([]ServerView, error) {
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	in.Name = strings.TrimSpace(in.Name)
	if in.Host == "" || in.User == "" {
		return nil, fmt.Errorf("host and user are required")
	}
	if in.Name == "" {
		in.Name = in.Host
	}
	if in.Port <= 0 {
		in.Port = 22
	}
	switch in.Auth {
	case "agent", "key", "password":
	default:
		in.Auth = "agent"
	}
	if in.ID == "" {
		in.ID = fmt.Sprintf("srv-%d", time.Now().UnixMilli())
	}
	p := store.ServerProfile{ID: in.ID, Name: in.Name, Host: in.Host, Port: in.Port, User: in.User, Auth: in.Auth, KeyPath: strings.TrimSpace(in.KeyPath), Password: in.Password, Dir: strings.TrimSpace(in.Dir), AddedAtMs: time.Now().UnixMilli()}
	if cur, err := s.profile(in.ID); err == nil {
		p.Actions, p.ClaudeMode, p.ClaudeSessionID = cur.Actions, cur.ClaudeMode, cur.ClaudeSessionID
	}
	if _, err := s.store.UpsertServer(p); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *ServerService) Remove(id string) ([]ServerView, error) {
	if _, err := s.store.RemoveServer(id); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *ServerService) profile(id string) (store.ServerProfile, error) {
	for _, p := range s.store.Read().Servers {
		if p.ID == id {
			return p, nil
		}
	}
	return store.ServerProfile{}, fmt.Errorf("unknown server %q", id)
}

func target(p store.ServerProfile) sshx.Target {
	return sshx.Target{Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, Password: p.Password}
}

// healthScript prints one KEY=value per line; everything is best effort.
const healthScript = `
echo "hostname=$(hostname 2>/dev/null)"
echo "user=$(id -un 2>/dev/null)"
echo "home=$HOME"
if [ -r /etc/os-release ]; then . /etc/os-release; echo "os=${PRETTY_NAME:-$NAME}"; else echo "os=$(uname -sr)"; fi
echo "kernel=$(uname -r 2>/dev/null)"
echo "uptime=$(uptime -p 2>/dev/null || uptime 2>/dev/null)"
echo "load=$(cut -d' ' -f1-3 /proc/loadavg 2>/dev/null || sysctl -n vm.loadavg 2>/dev/null)"
echo "disk=$(df -h / 2>/dev/null | awk 'NR==2{print $4" free of "$2" ("$5" used)"}')"
echo "memory=$(free -h 2>/dev/null | awk '/^Mem/{print $7" available of "$2}')"
echo "node=$(command -v node >/dev/null 2>&1 && node -v 2>/dev/null)"
echo "git=$(command -v git >/dev/null 2>&1 && git --version 2>/dev/null | sed 's/git version //')"
CL="$(command -v claude 2>/dev/null)"
[ -z "$CL" ] && [ -x "$HOME/.local/bin/claude" ] && CL="$HOME/.local/bin/claude"
echo "claude_path=$CL"
[ -n "$CL" ] && echo "claude_version=$("$CL" --version 2>/dev/null | head -1)"
[ -s "$HOME/.claude/.credentials.json" ] && echo "claude_creds=1"
[ -n "$ANTHROPIC_API_KEY" ] && echo "claude_apikey=1"
[ -r "$HOME/.claude.json" ] && echo "claude_account=$(grep -o '"emailAddress":"[^"]*"' "$HOME/.claude.json" 2>/dev/null | head -1 | cut -d'"' -f4)"
`

// Health connects and reports what is on the machine.
func (s *ServerService) Health(ctx context.Context, id string) ServerHealth {
	h := ServerHealth{CheckedAtMs: time.Now().UnixMilli()}
	p, err := s.profile(id)
	if err != nil {
		h.Error = err.Error()
		return h
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		h.Error = err.Error()
		h.Checks = []ServerCheck{{ID: "ssh", Label: "SSH connection", OK: false, Value: err.Error()}}
		return h
	}
	defer client.Close()
	res, err := sshx.Run(ctx, client, healthScript, 40*time.Second)
	if err != nil {
		h.Error = err.Error()
		h.Checks = []ServerCheck{{ID: "ssh", Label: "SSH connection", OK: true, Value: "connected"}, {ID: "script", Label: "Health script", OK: false, Value: err.Error()}}
		return h
	}
	kv := map[string]string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			kv[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	h.OK = true
	h.Hostname = kv["hostname"]
	h.OS = kv["os"]
	if kv["kernel"] != "" {
		h.OS = strings.TrimSpace(h.OS + " · " + kv["kernel"])
	}
	h.User = kv["user"]
	h.Home = kv["home"]
	h.Uptime = kv["uptime"]
	h.Load = kv["load"]
	h.Disk = kv["disk"]
	h.Memory = kv["memory"]
	h.Node = ServerTool{Installed: kv["node"] != "", Version: kv["node"]}
	h.Git = ServerTool{Installed: kv["git"] != "", Version: kv["git"]}
	h.Claude = ClaudeStatus{Installed: kv["claude_path"] != "", Version: kv["claude_version"], Path: kv["claude_path"], LoggedIn: kv["claude_creds"] == "1" || kv["claude_apikey"] == "1", Account: kv["claude_account"]}

	h.Checks = append(h.Checks, ServerCheck{ID: "ssh", Label: "SSH connection", OK: true, Value: fmt.Sprintf("%s@%s", h.User, h.Hostname)})
	h.Checks = append(h.Checks, ServerCheck{ID: "os", Label: "System", OK: true, Value: h.OS})
	if h.Disk != "" {
		h.Checks = append(h.Checks, ServerCheck{ID: "disk", Label: "Disk", OK: !strings.Contains(h.Disk, "9%") || strings.Contains(h.Disk, "(9%"), Value: h.Disk})
	}
	if h.Memory != "" {
		h.Checks = append(h.Checks, ServerCheck{ID: "mem", Label: "Memory", OK: true, Value: h.Memory})
	}
	h.Checks = append(h.Checks, ServerCheck{ID: "git", Label: "Git", OK: h.Git.Installed, Value: orDash(h.Git.Version), Hint: "Claude Code works better in a git checkout."})
	h.Checks = append(h.Checks, ServerCheck{ID: "node", Label: "Node.js", OK: h.Node.Installed, Value: orDash(h.Node.Version), Hint: "Not required by the native Claude Code installer, but many projects need it."})
	if h.Claude.Installed {
		h.Checks = append(h.Checks, ServerCheck{ID: "claude", Label: "Claude Code", OK: true, Value: fmt.Sprintf("%s (%s)", orDash(h.Claude.Version), h.Claude.Path)})
		switch {
		case h.Claude.LoggedIn && h.Claude.Account != "":
			h.Checks = append(h.Checks, ServerCheck{ID: "claude-login", Label: "Claude login", OK: true, Value: h.Claude.Account})
		case h.Claude.LoggedIn:
			h.Checks = append(h.Checks, ServerCheck{ID: "claude-login", Label: "Claude login", OK: true, Value: "credentials present"})
		default:
			h.Checks = append(h.Checks, ServerCheck{ID: "claude-login", Label: "Claude login", OK: false, Value: "not logged in", Hint: "Open the terminal and run: claude, then /login."})
		}
	} else {
		h.Checks = append(h.Checks, ServerCheck{ID: "claude", Label: "Claude Code", OK: false, Value: "not installed", Hint: "Install it from here; it goes to ~/.local/bin for this user."})
	}
	s.store.MarkServerOK(id)
	return h
}

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

// InstallClaude runs Anthropic's native installer for the SSH user and returns the
// tail of its output.
func (s *ServerService) InstallClaude(ctx context.Context, id string) (string, error) {
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return "", err
	}
	defer client.Close()
	res, err := sshx.Run(ctx, client, "curl -fsSL https://claude.ai/install.sh | bash 2>&1; echo \"exit=$?\"; export PATH=\"$HOME/.local/bin:$PATH\"; claude --version 2>&1", 6*time.Minute)
	if err != nil {
		return res.Stdout + res.Stderr, err
	}
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if len(out) > 6000 {
		out = "…" + out[len(out)-6000:]
	}
	return out, nil
}

// OpenShell starts an interactive login shell and streams it as ssh:out events.
func (s *ServerService) OpenShell(ctx context.Context, id string, cols, rows int) (string, error) {
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.seq++
	shellID := fmt.Sprintf("sh-%d-%d", time.Now().UnixMilli(), s.seq)
	s.mu.Unlock()
	sh, err := sshx.StartShell(client, cols, rows,
		func(b64 string) {
			if s.app != nil {
				s.app.Event.Emit("ssh:out", map[string]any{"id": shellID, "data": b64})
			}
		},
		func(exitErr error) {
			msg := ""
			if exitErr != nil {
				msg = exitErr.Error()
			}
			if s.app != nil {
				s.app.Event.Emit("ssh:exit", map[string]any{"id": shellID, "error": msg})
			}
			s.mu.Lock()
			delete(s.shells, shellID)
			s.mu.Unlock()
			_ = client.Close()
		})
	if err != nil {
		_ = client.Close()
		return "", err
	}
	s.mu.Lock()
	if s.shells == nil {
		s.shells = map[string]*openShell{}
	}
	s.shells[shellID] = &openShell{client: client, shell: sh}
	s.mu.Unlock()
	// Land in the project folder when one is set.
	if p.Dir != "" {
		_ = sh.Write(b64(fmt.Sprintf("cd %q && clear\n", p.Dir)))
	}
	log.Printf("servers: shell %s opened on %s", shellID, p.Host)
	return shellID, nil
}

func b64(s string) string {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	in := []byte(s)
	var out strings.Builder
	for i := 0; i < len(in); i += 3 {
		var n uint32
		rem := len(in) - i
		for j := 0; j < 3; j++ {
			n <<= 8
			if j < rem {
				n |= uint32(in[i+j])
			}
		}
		for j := 0; j < 4; j++ {
			if j <= rem {
				out.WriteByte(tbl[(n>>(18-6*j))&63])
			} else {
				out.WriteByte('=')
			}
		}
	}
	return out.String()
}

func (s *ServerService) get(shellID string) (*openShell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shells[shellID]
	if !ok {
		return nil, fmt.Errorf("that terminal is closed")
	}
	return sh, nil
}

func (s *ServerService) Write(shellID, b64Data string) error {
	sh, err := s.get(shellID)
	if err != nil {
		return err
	}
	return sh.shell.Write(b64Data)
}

func (s *ServerService) Resize(shellID string, cols, rows int) error {
	sh, err := s.get(shellID)
	if err != nil {
		return err
	}
	return sh.shell.Resize(cols, rows)
}

func (s *ServerService) CloseShell(shellID string) error {
	s.mu.Lock()
	sh, ok := s.shells[shellID]
	delete(s.shells, shellID)
	s.mu.Unlock()
	if ok {
		sh.shell.Close()
		_ = sh.client.Close()
	}
	return nil
}

// ---- quick actions ----------------------------------------------------------

// SaveAction adds or updates a saved command.
func (s *ServerService) SaveAction(serverID string, a store.ServerAction) ([]store.ServerAction, error) {
	p, err := s.profile(serverID)
	if err != nil {
		return nil, err
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Command = strings.TrimSpace(a.Command)
	if a.Name == "" || a.Command == "" {
		return nil, fmt.Errorf("a name and a command are required")
	}
	if a.ID == "" {
		a.ID = fmt.Sprintf("act-%d", time.Now().UnixMilli())
	}
	found := false
	for i := range p.Actions {
		if p.Actions[i].ID == a.ID {
			p.Actions[i] = a
			found = true
		}
	}
	if !found {
		p.Actions = append(p.Actions, a)
	}
	if _, err := s.store.SetServerActions(serverID, p.Actions); err != nil {
		return nil, err
	}
	return p.Actions, nil
}

func (s *ServerService) RemoveAction(serverID, actionID string) ([]store.ServerAction, error) {
	p, err := s.profile(serverID)
	if err != nil {
		return nil, err
	}
	kept := []store.ServerAction{}
	for _, a := range p.Actions {
		if a.ID != actionID {
			kept = append(kept, a)
		}
	}
	if _, err := s.store.SetServerActions(serverID, kept); err != nil {
		return nil, err
	}
	return kept, nil
}

// RunCommand runs one command on the server, streaming output as action:out and
// finishing with action:exit. Saved actions and one-off commands both use it.
func (s *ServerService) RunCommand(ctx context.Context, serverID, command string) (string, error) {
	p, err := s.profile(serverID)
	if err != nil {
		return "", err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", fmt.Errorf("nothing to run")
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.seq++
	runID := fmt.Sprintf("run-%d-%d", time.Now().UnixMilli(), s.seq)
	s.mu.Unlock()
	full := command
	if p.Dir != "" {
		full = "cd " + shq(p.Dir) + " 2>/dev/null; " + command
	}
	cmd, err := sshx.StartCommand(client, full,
		func(b64 string) {
			if s.app != nil {
				s.app.Event.Emit("action:out", map[string]any{"serverId": serverID, "runId": runID, "data": b64})
			}
		},
		func(code int, exitErr error) {
			msg := ""
			if exitErr != nil {
				msg = exitErr.Error()
			}
			if s.app != nil {
				s.app.Event.Emit("action:exit", map[string]any{"serverId": serverID, "runId": runID, "code": code, "error": msg})
			}
			s.mu.Lock()
			delete(s.cmds, runID)
			s.mu.Unlock()
			_ = client.Close()
		})
	if err != nil {
		_ = client.Close()
		return "", err
	}
	s.mu.Lock()
	if s.cmds == nil {
		s.cmds = map[string]*openCommand{}
	}
	s.cmds[runID] = &openCommand{client: client, cmd: cmd}
	s.mu.Unlock()
	log.Printf("servers: %s: running %q", p.Name, command)
	return runID, nil
}

func (s *ServerService) StopCommand(runID string) error {
	s.mu.Lock()
	c, ok := s.cmds[runID]
	s.mu.Unlock()
	if ok {
		c.cmd.Stop()
	}
	return nil
}
