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

// NodeConfig controls ClawHQ's node role: whether this machine exposes itself to
// agents, and which folders they may browse. Off by default — turning it on hands
// agents on the gateway reach into this computer.
type NodeConfig struct {
	Enabled       bool     `json:"enabled"`
	SharedFolders []string `json:"sharedFolders"`
	// DesktopControl lets computer.act drive this machine's mouse and keyboard.
	DesktopControl bool `json:"desktopControl,omitempty"`
}

type Config struct {
	Version int `json:"version"`
	// GatewayURL is the pre-multi-gateway field, kept only so old configs migrate.
	GatewayURL      string           `json:"gatewayUrl,omitempty"`
	Gateways        []GatewayProfile `json:"gateways"`
	ActiveGatewayID string           `json:"activeGatewayId"`
	Node            NodeConfig       `json:"node"`
	Departments     []Department     `json:"departments"`
	// Assignments maps agentId to departmentId. Agents with no entry are "Unassigned".
	Assignments map[string]string `json:"assignments"`
}

func defaults() Config {
	return Config{
		Version:  1,
		Gateways: []GatewayProfile{},
		Departments: []Department{
			{ID: "executive", Name: "Executive", Emoji: "🏛️", Order: 0},
			{ID: "marketing", Name: "Marketing", Emoji: "📣", Order: 1},
			{ID: "support", Name: "Support", Emoji: "🛟", Order: 2},
			{ID: "operations", Name: "Operations", Emoji: "⚙️", Order: 3},
		},
		Assignments: map[string]string{},
		Node:        NodeConfig{Enabled: false, SharedFolders: []string{}},
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
	if cfg.Departments == nil {
		cfg.Departments = defaults().Departments
	}
	if cfg.Gateways == nil {
		cfg.Gateways = []GatewayProfile{}
	}
	if cfg.Node.SharedFolders == nil {
		cfg.Node.SharedFolders = []string{}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.readLocked()
	if node.SharedFolders == nil {
		node.SharedFolders = []string{}
	}
	cfg.Node = node
	return s.writeLocked(cfg)
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
