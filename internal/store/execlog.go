package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ExecRecord is one command an agent asked this machine to run, with what was
// decided and what happened. The audit trail behind the approval banners.
type ExecRecord struct {
	ID         string `json:"id"`
	AtMs       int64  `json:"atMs"`
	AgentID    string `json:"agentId,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd,omitempty"`
	// Decision is why it ran or did not: allowlisted, allowed, always, approved-by-gateway,
	// run-without-asking, denied, timed-out, off, refused.
	Decision   string `json:"decision"`
	Ran        bool   `json:"ran"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	Success    bool   `json:"success"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	// Output is the tail of stdout and stderr, capped so the log stays small.
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

const (
	execLogCap    = 500
	execOutputCap = 4000
)

// ExecLog is the command history, stored beside clawhq.json.
type ExecLog struct {
	mu   sync.Mutex
	path string
	seq  int
}

// NewExecLog returns the log backed by ~/.openclaw/clawhq-exec-log.json.
func NewExecLog() (*ExecLog, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home directory: %w", err)
	}
	return &ExecLog{path: filepath.Join(home, ".openclaw", "clawhq-exec-log.json")}, nil
}

func (l *ExecLog) readLocked() []ExecRecord {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return []ExecRecord{}
	}
	var list []ExecRecord
	if err := json.Unmarshal(data, &list); err != nil || list == nil {
		return []ExecRecord{}
	}
	return list
}

func (l *ExecLog) writeLocked(list []ExecRecord) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// Append stores a record, newest first, trimming the output tail.
func (l *ExecLog) Append(r ExecRecord) (ExecRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	if r.ID == "" {
		r.ID = fmt.Sprintf("x-%d-%d", time.Now().UnixNano(), l.seq)
	}
	if r.AtMs == 0 {
		r.AtMs = time.Now().UnixMilli()
	}
	if len(r.Output) > execOutputCap {
		r.Output = "…" + r.Output[len(r.Output)-execOutputCap:]
	}
	list := append([]ExecRecord{r}, l.readLocked()...)
	if len(list) > execLogCap {
		list = list[:execLogCap]
	}
	return r, l.writeLocked(list)
}

// List returns every record, newest first.
func (l *ExecLog) List() []ExecRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readLocked()
}

// Clear removes everything.
func (l *ExecLog) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writeLocked([]ExecRecord{})
}
