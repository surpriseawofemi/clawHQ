// Package store holds ClawHQ's own config, kept beside OpenClaw's but never inside it.
//
// OpenClaw has no department concept, so departments and their agent assignments are
// ours to own. Writing them into openclaw.json would risk `openclaw configure` or schema
// validation stripping unknown keys, so they live in a separate file.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Department struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
	// Order is the sidebar position; lower sorts higher.
	Order int `json:"order"`
}

// GatewayProfile is one saved OpenClaw gateway.
//
// Only the address is kept here. Credentials never touch this file: the shared token
// is used once during pairing and then discarded, and the device token the gateway
// issues in exchange lives in the encrypted identity store.
type GatewayProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	// LastConnectedAtMs orders the list so the gateway you actually use floats up.
	LastConnectedAtMs int64 `json:"lastConnectedAtMs,omitempty"`
}

// Exec modes for agent commands run through the node role's system.run.
const (
	// ExecOff refuses every command.
	ExecOff = "off"
	// ExecAsk runs allowlisted commands and asks the user about the rest.
	ExecAsk = "ask"
	// ExecAllow runs everything without asking.
	ExecAllow = "allow"
)

// ExecConfig is the policy for commands agents run on this machine.
type ExecConfig struct {
	Mode string `json:"mode"`
	// Allow lists commands that run without a prompt. An entry matches the exact
	// command text, a prefix when it ends in "*", or the command's first word when
	// the entry is a bare program name such as "git".
	Allow []string `json:"allow"`
	// Agents overrides Mode for particular agents, by agent id: a trusted agent runs
	// without asking while a new one still asks. Absent means Mode applies.
	Agents map[string]string `json:"agents"`
}

// NormalizeExec fills defaults and drops overrides that are not a known mode.
func (e *ExecConfig) Normalize() {
	if e.Allow == nil {
		e.Allow = []string{}
	}
	switch e.Mode {
	case ExecOff, ExecAsk, ExecAllow:
	default:
		e.Mode = ExecAsk
	}
	agents := map[string]string{}
	for id, mode := range e.Agents {
		switch mode {
		case ExecOff, ExecAsk, ExecAllow:
			agents[id] = mode
		}
	}
	e.Agents = agents
}

// NodeConfig controls ClawHQ's node role: whether this machine exposes itself to
// agents, which folders they may browse, and what they may run. The role is on by
// default because ClawHQ pairs and approves it by itself; what it exposes is still
// gated feature by feature.
type NodeConfig struct {
	Enabled       bool     `json:"enabled"`
	SharedFolders []string `json:"sharedFolders"`
	// DesktopControl lets computer.act drive this machine's mouse and keyboard.
	DesktopControl bool `json:"desktopControl,omitempty"`
	// Exec is the policy for system.run. Defaults to asking.
	Exec ExecConfig `json:"exec"`
}

// configVersion is bumped when a saved config needs migrating on read.
const configVersion = 6

type Config struct {
	Version int `json:"version"`
	// GatewayURL is the pre-multi-gateway field, kept only so old configs migrate.
	GatewayURL      string           `json:"gatewayUrl,omitempty"`
	Gateways        []GatewayProfile `json:"gateways"`
	ActiveGatewayID string           `json:"activeGatewayId"`
	// AutoConnect reconnects to the last used gateway at launch. On by default.
	AutoConnect bool `json:"autoConnect"`
	// AutoUpdate downloads and installs a newer release as soon as one is found,
	// then relaunches. On by default.
	AutoUpdate bool `json:"autoUpdate"`
	// MenuBar keeps ClawHQ running in the menu bar (system tray) when its window is
	// closed, so the node role stays up. On by default.
	MenuBar     bool         `json:"menuBar"`
	Node        NodeConfig   `json:"node"`
	Departments []Department `json:"departments"`
	// Assignments maps agentId to departmentId. Agents with no entry are "Unassigned".
	Assignments map[string]string `json:"assignments"`
	// Servers are machines ClawHQ reaches over SSH: health, Claude Code, a terminal.
	// Local to this machine; the gateway never sees them.
	Servers []ServerProfile `json:"servers"`
	// InstanceID names this ClawHQ install to the gateway plugin (server tasks are
	// claimed by the install that owns the server).
	InstanceID string `json:"instanceId,omitempty"`
}

// ServerProfile is one SSH target. The password (or key passphrase) is kept in this
// file, which is 0600; prefer the agent or a key.
type ServerProfile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Auth     string `json:"auth"` // agent | key | password
	KeyPath  string `json:"keyPath,omitempty"`
	Password string `json:"password,omitempty"`
	// Dir is the folder Claude Code opens in by default.
	Dir        string `json:"dir,omitempty"`
	AddedAtMs  int64  `json:"addedAtMs"`
	LastOkAtMs int64  `json:"lastOkAtMs,omitempty"`
	// ClaudeMode is how headless Claude Code handles permissions here:
	// auto (never asks), semi (edits and safe reads allowed, the rest refused) or
	// manual (only reads). Empty means auto.
	ClaudeMode string `json:"claudeMode,omitempty"`
	// ClaudeSessionID is the Claude Code session the chat tab resumes.
	ClaudeSessionID string `json:"claudeSessionId,omitempty"`
	// Actions are saved commands run from a button.
	Actions []ServerAction `json:"actions,omitempty"`
	// Projects are folders on the server, each with its own agent, session and mode.
	Projects        []ServerProject `json:"projects,omitempty"`
	ActiveProjectID string          `json:"activeProjectId,omitempty"`
	// MonitorOff stops the periodic health check and its warnings.
	MonitorOff bool `json:"monitorOff,omitempty"`
}

// ServerProject is one folder on a server the coding agents work in.
type ServerProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dir  string `json:"dir"`
	// Agent drives the chat tab here: claude (default), codex, gemini or grok.
	Agent string `json:"agent,omitempty"`
	// Sessions holds the resumable session per agent.
	Sessions map[string]string `json:"sessions,omitempty"`
	// Mode is the permission mode: auto (default), semi or manual.
	Mode string `json:"mode,omitempty"`
}

// Active returns the current project, creating a view of the legacy fields when
// a server predates projects.
func (p ServerProfile) Active() ServerProject {
	for _, pr := range p.Projects {
		if pr.ID == p.ActiveProjectID {
			return pr
		}
	}
	if len(p.Projects) > 0 {
		return p.Projects[0]
	}
	return ServerProject{ID: "", Name: "Home", Dir: p.Dir, Agent: "claude", Sessions: map[string]string{"claude": p.ClaudeSessionID}, Mode: p.ClaudeMode}
}

// ServerAction is one saved command on a server.
type ServerAction struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	// Confirm asks before running (for restarts and the like).
	Confirm bool `json:"confirm"`
}

func defaults() Config {
	return Config{
		Version:     configVersion,
		Gateways:    []GatewayProfile{},
		AutoConnect: true,
		AutoUpdate:  true,
		MenuBar:     true,
		Departments: []Department{
			{ID: "executive", Name: "Executive", Emoji: "🏛️", Order: 0},
			{ID: "marketing", Name: "Marketing", Emoji: "📣", Order: 1},
			{ID: "support", Name: "Support", Emoji: "🛟", Order: 2},
			{ID: "operations", Name: "Operations", Emoji: "⚙️", Order: 3},
		},
		Assignments: map[string]string{},
		Node: NodeConfig{
			Enabled:       true,
			SharedFolders: []string{},
			Exec:          ExecConfig{Mode: ExecAsk, Allow: []string{}},
		},
	}
}

type Store struct {
	mu   sync.Mutex
	path string
}

// New returns a store backed by ~/.openclaw/clawhq.json.
func New() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home directory: %w", err)
	}
	return &Store{path: filepath.Join(home, ".openclaw", "clawhq.json")}, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Read() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *Store) readLocked() Config {
	cfg := defaults()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return cfg
	}
	// Unmarshal over the defaults so a partial file keeps sane values for the rest.
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaults()
	}
	if cfg.Assignments == nil {
		cfg.Assignments = map[string]string{}
	}
	for i := range cfg.Servers {
		sv := &cfg.Servers[i]
		if len(sv.Projects) == 0 {
			name := "Home"
			if sv.Dir != "" {
				name = filepath.Base(sv.Dir)
			}
			sv.Projects = []ServerProject{{ID: "proj-1", Name: name, Dir: sv.Dir, Agent: "claude", Sessions: map[string]string{"claude": sv.ClaudeSessionID}, Mode: sv.ClaudeMode}}
			sv.ActiveProjectID = "proj-1"
		}
		if sv.ActiveProjectID == "" {
			sv.ActiveProjectID = sv.Projects[0].ID
		}
		for j := range sv.Projects {
			if sv.Projects[j].Sessions == nil {
				sv.Projects[j].Sessions = map[string]string{}
			}
			if sv.Projects[j].Agent == "" {
				sv.Projects[j].Agent = "claude"
			}
		}
	}
	if cfg.Departments == nil {
		cfg.Departments = defaults().Departments
	}
	if cfg.Gateways == nil {
		cfg.Gateways = []GatewayProfile{}
	}
	if cfg.Node.SharedFolders == nil {
		cfg.Node.SharedFolders = []string{}
	}
	cfg.Node.Exec.Normalize()
	// Version 1 configs pre-date automatic node pairing, when the role stayed off
	// until the user pasted a token. Now that ClawHQ pairs itself, turn it on once;
	// the switch in Settings still turns it off for good.
	if cfg.Version < 2 {
		cfg.Node.Enabled = true
	}
	// Version 3 added the auto-connect switch; older files never had it off.
	if cfg.Version < 3 {
		cfg.AutoConnect = true
	}
	// Version 4 added the auto-update switch; older files never had it off.
	if cfg.Version < 4 {
		cfg.AutoUpdate = true
	}
	if cfg.Version < 5 {
		cfg.MenuBar = true
	}
	if cfg.Version < configVersion {
		cfg.Version = configVersion
	}
	// Migrate the single-gateway field into the list so existing installs keep their
	// connection without the user re-entering it.
	if len(cfg.Gateways) == 0 && cfg.GatewayURL != "" {
		cfg.Gateways = []GatewayProfile{{
			ID:   "default",
			Name: "OpenClaw",
			URL:  cfg.GatewayURL,
		}}
		cfg.ActiveGatewayID = "default"
	}
	return cfg
}

// ActiveGateway returns the currently selected gateway, falling back to the first
// saved one so a config with a stale active id still connects to something.
func (c Config) ActiveGateway() (GatewayProfile, bool) {
	for _, g := range c.Gateways {
		if g.ID == c.ActiveGatewayID {
			return g, true
		}
	}
	if len(c.Gateways) > 0 {
		return c.Gateways[0], true
	}
	return GatewayProfile{}, false
}

func (s *Store) writeLocked(cfg Config) (Config, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return cfg, err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return cfg, err
	}
	// Write-then-rename so a crash mid-write cannot leave a truncated config behind.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return cfg, err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// UpsertGateway adds or updates a gateway profile and makes it the active one.
// FindGatewayByURL returns the saved profile for a gateway URL, ignoring case,
// surrounding space, and a trailing slash. Device identities are keyed by profile
// ID, so pairing the same URL twice under two IDs means two devices the operator
// has to approve — and only the first one ever gets approved.
func (c Config) FindGatewayByURL(url string) (GatewayProfile, bool) {
	want := normalizeURL(url)
	if want == "" {
		return GatewayProfile{}, false
	}
	for _, g := range c.Gateways {
		if normalizeURL(g.URL) == want {
			return g, true
		}
	}
	return GatewayProfile{}, false
}

func normalizeURL(u string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(u), "/"))
}

func (s *Store) UpsertGateway(g GatewayProfile) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()

	if g.ID == "" {
		g.ID = newGatewayID(cfg.Gateways)
	}
	if g.Name == "" {
		g.Name = g.URL
	}

	replaced := false
	for i := range cfg.Gateways {
		if cfg.Gateways[i].ID == g.ID {
			// Preserve the recency stamp unless the caller set one.
			if g.LastConnectedAtMs == 0 {
				g.LastConnectedAtMs = cfg.Gateways[i].LastConnectedAtMs
			}
			cfg.Gateways[i] = g
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Gateways = append(cfg.Gateways, g)
	}
	cfg.ActiveGatewayID = g.ID
	cfg.GatewayURL = ""
	return s.writeLocked(cfg)
}

func (s *Store) RemoveGateway(id string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()

	kept := cfg.Gateways[:0]
	for _, g := range cfg.Gateways {
		if g.ID != id {
			kept = append(kept, g)
		}
	}
	cfg.Gateways = kept
	if cfg.ActiveGatewayID == id {
		cfg.ActiveGatewayID = ""
		if len(cfg.Gateways) > 0 {
			cfg.ActiveGatewayID = cfg.Gateways[0].ID
		}
	}
	return s.writeLocked(cfg)
}

// RenameGateway changes a profile's display name without touching which one is active.
func (s *Store) RenameGateway(id, name string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	name = strings.TrimSpace(name)
	for i := range cfg.Gateways {
		if cfg.Gateways[i].ID == id {
			if name == "" {
				name = cfg.Gateways[i].URL
			}
			cfg.Gateways[i].Name = name
			return s.writeLocked(cfg)
		}
	}
	return cfg, fmt.Errorf("no saved gateway with id %q", id)
}

// SetAutoUpdate stores whether new releases install themselves.
func (s *Store) SetAutoUpdate(on bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	cfg.AutoUpdate = on
	return s.writeLocked(cfg)
}

// SetMenuBar stores whether ClawHQ stays in the menu bar when its window closes.
func (s *Store) SetMenuBar(on bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	cfg.MenuBar = on
	return s.writeLocked(cfg)
}

// UpsertServer adds or updates an SSH server. An empty password on an update keeps
// the stored one, so editing a name never wipes a secret.
func (s *Store) UpsertServer(p ServerProfile) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i, cur := range cfg.Servers {
		if cur.ID == p.ID {
			if p.Password == "" {
				p.Password = cur.Password
			}
			if p.AddedAtMs == 0 {
				p.AddedAtMs = cur.AddedAtMs
			}
			cfg.Servers[i] = p
			return s.writeLocked(cfg)
		}
	}
	cfg.Servers = append(cfg.Servers, p)
	return s.writeLocked(cfg)
}

// RemoveServer forgets an SSH server.
func (s *Store) RemoveServer(id string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	kept := cfg.Servers[:0]
	for _, cur := range cfg.Servers {
		if cur.ID != id {
			kept = append(kept, cur)
		}
	}
	cfg.Servers = kept
	return s.writeLocked(cfg)
}

// UpdateProject edits a project's session (per agent), mode or agent on a server.
func (s *Store) UpdateProject(serverID, projectID string, fn func(*ServerProject)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID != serverID {
			continue
		}
		sv := &cfg.Servers[i]
		if projectID == "" {
			projectID = sv.ActiveProjectID
		}
		for j := range sv.Projects {
			if sv.Projects[j].ID == projectID {
				fn(&sv.Projects[j])
				if sv.Projects[j].ID == sv.ActiveProjectID {
					// Legacy fields mirror the active project.
					sv.Dir = sv.Projects[j].Dir
					sv.ClaudeSessionID = sv.Projects[j].Sessions["claude"]
					sv.ClaudeMode = sv.Projects[j].Mode
				}
			}
		}
	}
	return s.writeLocked(cfg)
}

// AddProject adds a folder to a server and makes it active.
func (s *Store) AddProject(serverID string, pr ServerProject) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID != serverID {
			continue
		}
		sv := &cfg.Servers[i]
		if pr.ID == "" {
			pr.ID = fmt.Sprintf("proj-%d", time.Now().UnixMilli())
		}
		if pr.Sessions == nil {
			pr.Sessions = map[string]string{}
		}
		if pr.Agent == "" {
			pr.Agent = "claude"
		}
		sv.Projects = append(sv.Projects, pr)
		sv.ActiveProjectID = pr.ID
		sv.Dir, sv.ClaudeSessionID, sv.ClaudeMode = pr.Dir, pr.Sessions["claude"], pr.Mode
	}
	return s.writeLocked(cfg)
}

// RemoveProject drops a folder; the first remaining one becomes active.
func (s *Store) RemoveProject(serverID, projectID string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID != serverID {
			continue
		}
		sv := &cfg.Servers[i]
		kept := sv.Projects[:0]
		for _, pr := range sv.Projects {
			if pr.ID != projectID {
				kept = append(kept, pr)
			}
		}
		sv.Projects = kept
		if len(sv.Projects) > 0 && sv.ActiveProjectID == projectID {
			sv.ActiveProjectID = sv.Projects[0].ID
			a := sv.Projects[0]
			sv.Dir, sv.ClaudeSessionID, sv.ClaudeMode = a.Dir, a.Sessions["claude"], a.Mode
		}
	}
	return s.writeLocked(cfg)
}

// SelectProject makes a folder the active one for a server.
func (s *Store) SelectProject(serverID, projectID string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID != serverID {
			continue
		}
		sv := &cfg.Servers[i]
		for _, pr := range sv.Projects {
			if pr.ID == projectID {
				sv.ActiveProjectID = pr.ID
				sv.Dir, sv.ClaudeSessionID, sv.ClaudeMode = pr.Dir, pr.Sessions["claude"], pr.Mode
			}
		}
	}
	return s.writeLocked(cfg)
}

// EnsureInstanceID returns this install's id, minting one the first time.
func (s *Store) EnsureInstanceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	if cfg.InstanceID != "" {
		return cfg.InstanceID
	}
	host, _ := os.Hostname()
	cfg.InstanceID = fmt.Sprintf("%s-%d", strings.ToLower(strings.SplitN(host, ".", 2)[0]), time.Now().UnixMilli()%1000000)
	_, _ = s.writeLocked(cfg)
	return cfg.InstanceID
}

// SetServerMonitor turns the periodic health check on or off.
func (s *Store) SetServerMonitor(serverID string, on bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID == serverID {
			cfg.Servers[i].MonitorOff = !on
		}
	}
	return s.writeLocked(cfg)
}

// SetServerActions replaces a server's saved commands.
func (s *Store) SetServerActions(id string, actions []ServerAction) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID == id {
			cfg.Servers[i].Actions = actions
		}
	}
	return s.writeLocked(cfg)
}

// MarkServerOK records a successful health check.
func (s *Store) MarkServerOK(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Servers {
		if cfg.Servers[i].ID == id {
			cfg.Servers[i].LastOkAtMs = time.Now().UnixMilli()
		}
	}
	_, _ = s.writeLocked(cfg)
}

// SetAutoConnect stores whether ClawHQ reconnects to the last gateway at launch.
func (s *Store) SetAutoConnect(on bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	cfg.AutoConnect = on
	return s.writeLocked(cfg)
}

func (s *Store) SetActiveGateway(id string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	cfg.ActiveGatewayID = id
	return s.writeLocked(cfg)
}

// TouchGateway records a successful connection, used only for ordering the list.
func (s *Store) TouchGateway(id string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Gateways {
		if cfg.Gateways[i].ID == id {
			cfg.Gateways[i].LastConnectedAtMs = time.Now().UnixMilli()
		}
	}
	return s.writeLocked(cfg)
}

// newGatewayID returns a short id that does not collide with the existing profiles.
func newGatewayID(existing []GatewayProfile) string {
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("gw%d", i)
		taken := false
		for _, g := range existing {
			if g.ID == candidate {
				taken = true
				break
			}
		}
		if !taken {
			return candidate
		}
	}
}

// SetNodeConfig stores the node role settings.
func (s *Store) SetNodeConfig(node NodeConfig) (Config, error) {
	return s.UpdateNode(func(n *NodeConfig) { *n = node })
}

// UpdateNode applies a change to the node settings under the lock, so a caller that
// touches one field cannot clobber another written at the same time.
func (s *Store) UpdateNode(mutate func(*NodeConfig)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	mutate(&cfg.Node)
	if cfg.Node.SharedFolders == nil {
		cfg.Node.SharedFolders = []string{}
	}
	cfg.Node.Exec.Normalize()
	return s.writeLocked(cfg)
}

// SetAgentExecMode overrides the exec mode for one agent; an empty mode removes the
// override so the machine-wide mode applies again.
func (s *Store) SetAgentExecMode(agentID, mode string) (Config, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return s.Read(), nil
	}
	return s.UpdateNode(func(n *NodeConfig) {
		if n.Exec.Agents == nil {
			n.Exec.Agents = map[string]string{}
		}
		if mode == "" {
			delete(n.Exec.Agents, agentID)
		} else {
			n.Exec.Agents[agentID] = mode
		}
	})
}

// AllowExecCommand adds an entry to the exec allowlist, ignoring duplicates.
func (s *Store) AllowExecCommand(entry string) (Config, error) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return s.Read(), nil
	}
	return s.UpdateNode(func(n *NodeConfig) {
		for _, existing := range n.Exec.Allow {
			if existing == entry {
				return
			}
		}
		n.Exec.Allow = append(n.Exec.Allow, entry)
	})
}

// AssignAgent files an agent into a department, or removes the assignment when
// departmentID is empty.
func (s *Store) AssignAgent(agentID, departmentID string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	if departmentID == "" {
		delete(cfg.Assignments, agentID)
	} else {
		cfg.Assignments[agentID] = departmentID
	}
	return s.writeLocked(cfg)
}

// ReplaceOrg makes the local org chart a mirror of one kept elsewhere (the ClawHQ
// gateway plugin). Nil slices and maps are treated as empty.
func (s *Store) ReplaceOrg(departments []Department, assignments map[string]string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	cfg.Departments = append([]Department{}, departments...)
	cfg.Assignments = map[string]string{}
	for k, v := range assignments {
		cfg.Assignments[k] = v
	}
	return s.writeLocked(cfg)
}

func (s *Store) UpsertDepartment(dept Department) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	for i := range cfg.Departments {
		if cfg.Departments[i].ID == dept.ID {
			// Preserve position unless the caller deliberately set one.
			if dept.Order == 0 {
				dept.Order = cfg.Departments[i].Order
			}
			cfg.Departments[i] = dept
			return s.writeLocked(cfg)
		}
	}
	if dept.Order == 0 {
		dept.Order = len(cfg.Departments)
	}
	cfg.Departments = append(cfg.Departments, dept)
	return s.writeLocked(cfg)
}

func (s *Store) RemoveDepartment(id string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()

	kept := cfg.Departments[:0]
	for _, d := range cfg.Departments {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	cfg.Departments = kept

	// Orphaned agents fall back to "Unassigned" rather than vanishing from the sidebar.
	for agentID, deptID := range cfg.Assignments {
		if deptID == id {
			delete(cfg.Assignments, agentID)
		}
	}
	return s.writeLocked(cfg)
}
