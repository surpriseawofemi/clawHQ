package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
	serverID string
	client   *ssh.Client
	shell    *sshx.Shell
}

type openCommand struct {
	serverID string
	client   *ssh.Client
	cmd      *sshx.Command
}

// ServerView is a profile without its secret.
type ServerView struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Host            string                `json:"host"`
	Port            int                   `json:"port"`
	User            string                `json:"user"`
	Auth            string                `json:"auth"`
	KeyPath         string                `json:"keyPath,omitempty"`
	HasPassword     bool                  `json:"hasPassword"`
	Dir             string                `json:"dir,omitempty"`
	AddedAtMs       int64                 `json:"addedAtMs"`
	LastOkAtMs      int64                 `json:"lastOkAtMs,omitempty"`
	Actions         []store.ServerAction  `json:"actions"`
	Projects        []store.ServerProject `json:"projects"`
	ActiveProjectID string                `json:"activeProjectId"`
	Monitor         bool                  `json:"monitor"`
	Tmux            bool                  `json:"tmux"`
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
	Monitor  *bool  `json:"monitor"`
	Tmux     *bool  `json:"tmux"`
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

// AgentStatus is one coding agent CLI on the server.
type AgentStatus struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	LoggedIn  bool   `json:"loggedIn"`
	Account   string `json:"account,omitempty"`
	Install   string `json:"install"`
	LoginHint string `json:"loginHint"`
}

type ServerHealth struct {
	Tmux        ServerTool    `json:"tmux"`
	Agents      []AgentStatus `json:"agents"`
	Cores       int           `json:"cores"`
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
	projects := p.Projects
	if projects == nil {
		projects = []store.ServerProject{}
	}
	return ServerView{ID: p.ID, Name: p.Name, Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, HasPassword: p.Password != "", Dir: p.Active().Dir, AddedAtMs: p.AddedAtMs, LastOkAtMs: p.LastOkAtMs, Actions: actions, Projects: projects, ActiveProjectID: p.ActiveProjectID, Monitor: !p.MonitorOff, Tmux: !p.TmuxOff}
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
		p.Projects, p.ActiveProjectID, p.MonitorOff = cur.Projects, cur.ActiveProjectID, cur.MonitorOff
		if p.Dir != "" && p.Dir != cur.Active().Dir {
			for i := range p.Projects {
				if p.Projects[i].ID == p.ActiveProjectID {
					p.Projects[i].Dir = p.Dir
				}
			}
		}
	}
	if in.Monitor != nil {
		p.MonitorOff = !*in.Monitor
	}
	if cur, err := s.profile(in.ID); err == nil {
		p.TmuxOff = cur.TmuxOff
	}
	if in.Tmux != nil {
		p.TmuxOff = !*in.Tmux
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
echo "cores=$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null)"
echo "tmux=$(command -v tmux >/dev/null 2>&1 && tmux -V 2>/dev/null | sed 's/^tmux //')"
echo "node=$(command -v node >/dev/null 2>&1 && node -v 2>/dev/null)"
export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$PATH"
CX="$(command -v codex 2>/dev/null)"; echo "codex_path=$CX"; [ -n "$CX" ] && echo "codex_version=$("$CX" --version 2>/dev/null | head -1)"
[ -s "$HOME/.codex/auth.json" ] && echo "codex_login=1"; [ -n "$OPENAI_API_KEY" ] && echo "codex_login=1"
GM="$(command -v gemini 2>/dev/null)"; echo "gemini_path=$GM"; [ -n "$GM" ] && echo "gemini_version=$("$GM" --version 2>/dev/null | head -1)"
[ -s "$HOME/.gemini/oauth_creds.json" ] && echo "gemini_login=1"; [ -n "$GEMINI_API_KEY" ] && echo "gemini_login=1"; [ -n "$GOOGLE_API_KEY" ] && echo "gemini_login=1"
GK="$(command -v grok 2>/dev/null)"; echo "grok_path=$GK"; [ -n "$GK" ] && echo "grok_version=$("$GK" --version 2>/dev/null | head -1)"
[ -s "$HOME/.grok/user-settings.json" ] && grep -q apiKey "$HOME/.grok/user-settings.json" 2>/dev/null && echo "grok_login=1"; [ -n "$GROK_API_KEY" ] && echo "grok_login=1"
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
	fmt.Sscanf(kv["cores"], "%d", &h.Cores)
	h.Tmux = ServerTool{Installed: kv["tmux"] != "", Version: kv["tmux"]}
	h.Agents = []AgentStatus{
		{ID: "claude", Label: "Claude Code", Installed: h.Claude.Installed, Version: h.Claude.Version, Path: h.Claude.Path, LoggedIn: h.Claude.LoggedIn, Account: h.Claude.Account, Install: "curl -fsSL https://claude.ai/install.sh | bash", LoginHint: "run: claude, then /login"},
		{ID: "codex", Label: "Codex", Installed: kv["codex_path"] != "", Version: kv["codex_version"], Path: kv["codex_path"], LoggedIn: kv["codex_login"] == "1", Install: "npm install -g @openai/codex", LoginHint: "run: codex login"},
		{ID: "gemini", Label: "Gemini CLI", Installed: kv["gemini_path"] != "", Version: kv["gemini_version"], Path: kv["gemini_path"], LoggedIn: kv["gemini_login"] == "1", Install: "npm install -g @google/gemini-cli", LoginHint: "run: gemini, then sign in"},
		{ID: "grok", Label: "Grok CLI", Installed: kv["grok_path"] != "", Version: kv["grok_version"], Path: kv["grok_path"], LoggedIn: kv["grok_login"] == "1", Install: "npm install -g @vibe-kit/grok-cli", LoginHint: "set GROK_API_KEY or run: grok and enter the key"},
	}

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
	h.Checks = append(h.Checks, ServerCheck{ID: "tmux", Label: "tmux", OK: h.Tmux.Installed, Value: orDash(h.Tmux.Version), Hint: "Keeps terminals alive across ClawHQ restarts. Install it from here."})
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

// InstallAgent installs one of the coding agent CLIs for the SSH user and returns the tail of the output.
func (s *ServerService) InstallAgent(ctx context.Context, id, agent string) (string, error) {
	var cmd string
	switch agent {
	case "claude":
		return s.InstallClaude(ctx, id)
	case "codex":
		cmd = "npm install -g @openai/codex 2>&1; echo \"exit=$?\"; codex --version 2>&1"
	case "gemini":
		cmd = "npm install -g @google/gemini-cli 2>&1; echo \"exit=$?\"; gemini --version 2>&1"
	case "grok":
		cmd = "npm install -g @vibe-kit/grok-cli 2>&1; echo \"exit=$?\"; grok --version 2>&1"
	default:
		return "", fmt.Errorf("unknown agent %q", agent)
	}
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return "", err
	}
	defer client.Close()
	res, err := sshx.Run(ctx, client, "command -v npm >/dev/null 2>&1 || { echo 'npm is not installed on this server; install Node.js first'; exit 1; }; "+cmd, 8*time.Minute)
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if len(out) > 6000 {
		out = "…" + out[len(out)-6000:]
	}
	return out, err
}

// ---- projects ----------------------------------------------------------------

func (s *ServerService) AddProject(id, name, dir, agent string) ([]ServerView, error) {
	name, dir = strings.TrimSpace(name), strings.TrimSpace(dir)
	if name == "" {
		name = dir
	}
	if name == "" {
		return nil, fmt.Errorf("a name or folder is required")
	}
	if _, err := s.store.AddProject(id, store.ServerProject{Name: name, Dir: dir, Agent: agent}); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *ServerService) UpdateProject(id, projectID, name, dir string) ([]ServerView, error) {
	if _, err := s.store.UpdateProject(id, projectID, func(pr *store.ServerProject) {
		if strings.TrimSpace(name) != "" {
			pr.Name = strings.TrimSpace(name)
		}
		pr.Dir = strings.TrimSpace(dir)
	}); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *ServerService) RemoveProject(id, projectID string) ([]ServerView, error) {
	if _, err := s.store.RemoveProject(id, projectID); err != nil {
		return nil, err
	}
	return s.List(), nil
}

func (s *ServerService) SelectProject(id, projectID string) ([]ServerView, error) {
	if _, err := s.store.SelectProject(id, projectID); err != nil {
		return nil, err
	}
	return s.List(), nil
}

// OpenShell starts an interactive login shell in the active project and streams it as ssh:out events.
func (s *ServerService) OpenShell(ctx context.Context, id string, cols, rows int) (string, error) {
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	return s.OpenShellIn(ctx, id, p.Active().Dir, cols, rows, "")
}

// OpenShellIn starts a login shell in a folder. With a session name and tmux on
// the server, the shell lives in a tmux session that survives ClawHQ closing:
// reopening with the same name attaches to it.
func (s *ServerService) OpenShellIn(ctx context.Context, id, dir string, cols, rows int, session string) (string, error) {
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
	command := ""
	if session != "" && !p.TmuxOff {
		name := tmuxNameRe.ReplaceAllString(session, "-")
		cd := ""
		if dir != "" {
			cd = "cd " + shq(dir) + " 2>/dev/null; "
		}
		// -A attaches when the session exists; the chained commands set mouse
		// scrolling, a deep history, and hide tmux's status bar (ClawHQ has tabs).
		command = "bash -lc " + shq(cd+"if command -v tmux >/dev/null 2>&1; then exec tmux -u new-session -A -s "+shq(name)+" \\; set -g mouse on \\; set -g history-limit 20000 \\; set -g status off; else exec bash -l; fi")
	}
	sh, err := sshx.StartShellCmd(client, cols, rows, command,
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
	s.shells[shellID] = &openShell{serverID: id, client: client, shell: sh}
	s.mu.Unlock()
	// A plain shell lands in the project folder; tmux was started there already.
	if dir != "" && command == "" {
		_ = sh.Write(b64(fmt.Sprintf("cd %q && clear\n", dir)))
	}
	log.Printf("servers: shell %s opened on %s", shellID, p.Host)
	return shellID, nil
}

var tmuxNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// TmuxSessions lists tmux sessions on the server (name, windows, attached).
func (s *ServerService) TmuxSessions(ctx context.Context, id string) ([]string, error) {
	p, err := s.profile(id)
	if err != nil {
		return nil, err
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return nil, err
	}
	defer client.Close()
	res, err := sshx.Run(ctx, client, "tmux ls -F '#{session_name}' 2>/dev/null", 15*time.Second)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, l := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// KillTmux ends a tmux session on the server (closing a tab for good).
func (s *ServerService) KillTmux(ctx context.Context, id, session string) error {
	p, err := s.profile(id)
	if err != nil {
		return err
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return err
	}
	defer client.Close()
	name := tmuxNameRe.ReplaceAllString(session, "-")
	_, err = sshx.Run(ctx, client, "tmux kill-session -t "+shq(name)+" 2>/dev/null; true", 15*time.Second)
	return err
}

// InstallTmux installs tmux with the server's package manager.
func (s *ServerService) InstallTmux(ctx context.Context, id string) (string, error) {
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return "", err
	}
	defer client.Close()
	cmd := `if command -v apt-get >/dev/null 2>&1; then sudo apt-get install -y tmux 2>&1; elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y tmux 2>&1; elif command -v yum >/dev/null 2>&1; then sudo yum install -y tmux 2>&1; elif command -v brew >/dev/null 2>&1; then brew install tmux 2>&1; else echo "no known package manager"; exit 1; fi; tmux -V`
	res, err := sshx.Run(ctx, client, cmd, 5*time.Minute)
	out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	if len(out) > 4000 {
		out = "…" + out[len(out)-4000:]
	}
	return out, err
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

// RunCommand runs one command on the server in the active project.
func (s *ServerService) RunCommand(ctx context.Context, serverID, command string) (string, error) {
	p, err := s.profile(serverID)
	if err != nil {
		return "", err
	}
	return s.RunCommandIn(ctx, serverID, command, p.Active().Dir)
}

// RunCommandIn runs one command in a folder, streaming output as action:out and
// finishing with action:exit. Saved actions and one-off commands both use it.
func (s *ServerService) RunCommandIn(ctx context.Context, serverID, command, dir string) (string, error) {
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
	if dir != "" {
		full = "cd " + shq(dir) + " 2>/dev/null; " + command
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
	s.cmds[runID] = &openCommand{serverID: serverID, client: client, cmd: cmd}
	s.mu.Unlock()
	log.Printf("servers: %s: running %q", p.Name, command)
	return runID, nil
}

// Disconnect closes every SSH connection ClawHQ holds to a server: shells, running
// commands and the SFTP link. tmux sessions on the server keep running.
func (s *ServerService) Disconnect(id string) error {
	s.mu.Lock()
	var shells []*openShell
	var cmds []*openCommand
	for k, sh := range s.shells {
		if sh.serverID == id {
			shells = append(shells, sh)
			delete(s.shells, k)
		}
	}
	for k, c := range s.cmds {
		if c.serverID == id {
			cmds = append(cmds, c)
			delete(s.cmds, k)
		}
	}
	s.mu.Unlock()
	for _, sh := range shells {
		sh.shell.Close()
		_ = sh.client.Close()
	}
	for _, c := range cmds {
		c.cmd.Stop()
		_ = c.client.Close()
	}
	if s.files != nil {
		s.files.drop(id)
	}
	if s.claude != nil {
		_ = s.claude.Abort(id)
	}
	log.Printf("servers: disconnected from %s", id)
	return nil
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

// ---- what is in a project folder -----------------------------------------------

type ProjectInfo struct {
	Dir       string               `json:"dir"`
	Exists    bool                 `json:"exists"`
	GitRemote string               `json:"gitRemote,omitempty"`
	GitBranch string               `json:"gitBranch,omitempty"`
	GitDirty  int                  `json:"gitDirty"`
	Package   string               `json:"package,omitempty"` // package.json name
	Scripts   []string             `json:"scripts"`           // package.json script names
	GoModule  string               `json:"goModule,omitempty"`
	Composer  string               `json:"composer,omitempty"`
	Python    bool                 `json:"python"`
	Docker    bool                 `json:"docker"`
	Pm2       string               `json:"pm2,omitempty"`
	HasClaude bool                 `json:"hasClaudeMd"`
	HasEnv    bool                 `json:"hasEnv"`
	Files     int                  `json:"files"`
	Suggested []store.ServerAction `json:"suggested"`
}

const projectInfoScript = `
D=%s
[ -d "$D" ] || { echo "exists=0"; exit 0; }
cd "$D" || exit 0
echo "exists=1"
echo "files=$(ls -1A 2>/dev/null | wc -l | tr -d ' ')"
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "git_remote=$(git remote get-url origin 2>/dev/null)"
  echo "git_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
  echo "git_dirty=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
fi
if [ -f package.json ]; then
  echo "package=$(node -e 'try{console.log(require("./package.json").name||"")}catch(e){}' 2>/dev/null)"
  echo "scripts=$(node -e 'try{console.log(Object.keys(require("./package.json").scripts||{}).join(","))}catch(e){}' 2>/dev/null)"
fi
[ -f go.mod ] && echo "go_module=$(head -1 go.mod | sed 's/^module //')"
[ -f composer.json ] && echo "composer=$(grep -o '"name": *"[^"]*"' composer.json | head -1 | cut -d'"' -f4)"
{ [ -f requirements.txt ] || [ -f pyproject.toml ]; } && echo "python=1"
{ [ -f Dockerfile ] || [ -f docker-compose.yml ] || [ -f compose.yaml ]; } && echo "docker=1"
[ -f ecosystem.config.js ] && echo "pm2=$(grep -o "name: *['\"][^'\"]*['\"]" ecosystem.config.js | head -1 | sed "s/name: *//; s/['\"]//g")"
[ -f CLAUDE.md ] && echo "claude_md=1"
[ -f .env ] && echo "env=1"
`

// ProjectInfo reads a folder once and suggests actions for it.
func (s *ServerService) ProjectInfo(ctx context.Context, id, dir string) (ProjectInfo, error) {
	p, err := s.profile(id)
	if err != nil {
		return ProjectInfo{}, err
	}
	info := ProjectInfo{Dir: dir, Scripts: []string{}, Suggested: []store.ServerAction{}}
	if strings.TrimSpace(dir) == "" {
		return info, nil
	}
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return info, err
	}
	defer client.Close()
	res, err := sshx.Run(ctx, client, fmt.Sprintf(projectInfoScript, shq(dir)), 30*time.Second)
	if err != nil {
		return info, err
	}
	kv := map[string]string{}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			kv[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	info.Exists = kv["exists"] == "1"
	fmt.Sscanf(kv["files"], "%d", &info.Files)
	info.GitRemote, info.GitBranch = kv["git_remote"], kv["git_branch"]
	fmt.Sscanf(kv["git_dirty"], "%d", &info.GitDirty)
	info.Package = kv["package"]
	if kv["scripts"] != "" {
		info.Scripts = strings.Split(kv["scripts"], ",")
	}
	info.GoModule, info.Composer, info.Pm2 = kv["go_module"], kv["composer"], kv["pm2"]
	info.Python, info.Docker = kv["python"] == "1", kv["docker"] == "1"
	info.HasClaude, info.HasEnv = kv["claude_md"] == "1", kv["env"] == "1"

	add := func(name, cmd string, confirm bool) {
		info.Suggested = append(info.Suggested, store.ServerAction{Name: name, Command: cmd, Confirm: confirm})
	}
	if info.GitRemote != "" || info.GitBranch != "" {
		add("Git status", "git status --short --branch", false)
		add("Git pull", "git pull --ff-only", true)
	}
	has := func(name string) bool {
		for _, x := range info.Scripts {
			if x == name {
				return true
			}
		}
		return false
	}
	if info.Package != "" {
		if has("test") {
			add("npm test", "npm test", false)
		}
		if has("build") {
			add("npm run build", "npm run build", false)
		}
		if has("lint") {
			add("npm run lint", "npm run lint", false)
		}
		add("npm install", "npm install", true)
	}
	if info.GoModule != "" {
		add("go build", "go build ./...", false)
		add("go test", "go test ./...", false)
		add("go vet", "go vet ./...", false)
	}
	if info.Composer != "" {
		add("composer install", "composer install --no-interaction", true)
	}
	if info.Python {
		add("pytest", "pytest -q", false)
	}
	if info.Docker {
		add("docker compose ps", "docker compose ps", false)
		add("docker compose up -d", "docker compose up -d", true)
		add("docker compose logs", "docker compose logs --tail=100", false)
	}
	if info.Pm2 != "" {
		add("pm2 restart "+info.Pm2, "pm2 restart "+shq(info.Pm2), true)
		add("pm2 logs "+info.Pm2, "pm2 logs "+shq(info.Pm2)+" --lines 100 --nostream", false)
	} else if info.Package != "" {
		add("pm2 list", "pm2 list", false)
	}
	return info, nil
}
