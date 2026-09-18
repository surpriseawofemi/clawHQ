package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// ServerRun is one headless agent run on a server, kept for the digest.
type ServerRun struct {
	ServerID   string  `json:"serverId"`
	ServerName string  `json:"serverName"`
	ProjectID  string  `json:"projectId"`
	Project    string  `json:"project"`
	Agent      string  `json:"agent"`
	AtMs       int64   `json:"atMs"`
	DurationMs int64   `json:"durationMs"`
	CostUsd    float64 `json:"costUsd"`
	Turns      int     `json:"turns"`
	OK         bool    `json:"ok"`
	Summary    string  `json:"summary"`
	Source     string  `json:"source"` // chat | task
}

// RunLog is ~/.openclaw/clawhq-server-runs.json, capped.
type RunLog struct {
	mu   sync.Mutex
	path string
}

func NewRunLog() (*RunLog, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &RunLog{path: filepath.Join(home, ".openclaw", "clawhq-server-runs.json")}, nil
}

const runLogCap = 3000

func (l *RunLog) read() []ServerRun {
	b, err := os.ReadFile(l.path)
	if err != nil {
		return nil
	}
	var out []ServerRun
	_ = json.Unmarshal(b, &out)
	return out
}

func (l *RunLog) Append(r ServerRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := append(l.read(), r)
	if len(list) > runLogCap {
		list = list[len(list)-runLogCap:]
	}
	b, _ := json.Marshal(list)
	_ = os.MkdirAll(filepath.Dir(l.path), 0o700)
	_ = os.WriteFile(l.path, b, 0o600)
}

// Since returns runs at or after a time, newest last.
func (l *RunLog) Since(atMs int64) []ServerRun {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []ServerRun{}
	for _, r := range l.read() {
		if r.AtMs >= atMs {
			out = append(out, r)
		}
	}
	return out
}
