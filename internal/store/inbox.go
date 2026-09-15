package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Notice is one notification an agent sent to this machine, kept so a request
// made while nobody was looking is still there later.
type Notice struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	AgentID    string `json:"agentId,omitempty"`
	AgentName  string `json:"agentName,omitempty"`
	AgentEmoji string `json:"agentEmoji,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
	AtMs       int64  `json:"atMs"`
	Read       bool   `json:"read"`
}

// inboxCap bounds the file; older entries fall off the end.
const inboxCap = 500

// Inbox is the notification history, stored beside clawhq.json.
type Inbox struct {
	mu   sync.Mutex
	path string
	seq  int
}

// NewInbox returns the inbox backed by ~/.openclaw/clawhq-notifications.json.
func NewInbox() (*Inbox, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home directory: %w", err)
	}
	return &Inbox{path: filepath.Join(home, ".openclaw", "clawhq-notifications.json")}, nil
}

func (b *Inbox) readLocked() []Notice {
	data, err := os.ReadFile(b.path)
	if err != nil {
		return []Notice{}
	}
	var list []Notice
	if err := json.Unmarshal(data, &list); err != nil || list == nil {
		return []Notice{}
	}
	return list
}

func (b *Inbox) writeLocked(list []Notice) error {
	if err := os.MkdirAll(filepath.Dir(b.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := b.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.path)
}

// Append stores a notice, newest first, and returns it with its id filled in.
func (b *Inbox) Append(n Notice) (Notice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	if n.ID == "" {
		n.ID = fmt.Sprintf("n-%d-%d", time.Now().UnixNano(), b.seq)
	}
	if n.AtMs == 0 {
		n.AtMs = time.Now().UnixMilli()
	}
	list := append([]Notice{n}, b.readLocked()...)
	if len(list) > inboxCap {
		list = list[:inboxCap]
	}
	return n, b.writeLocked(list)
}

// List returns every notice, newest first.
func (b *Inbox) List() []Notice {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.readLocked()
}

// Unread counts notices nobody has looked at.
func (b *Inbox) Unread() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, x := range b.readLocked() {
		if !x.Read {
			n++
		}
	}
	return n
}

// MarkAllRead flags every notice as seen.
func (b *Inbox) MarkAllRead() ([]Notice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.readLocked()
	for i := range list {
		list[i].Read = true
	}
	return list, b.writeLocked(list)
}

// MarkRead flags one notice as seen.
func (b *Inbox) MarkRead(id string) ([]Notice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.readLocked()
	for i := range list {
		if list[i].ID == id {
			list[i].Read = true
		}
	}
	return list, b.writeLocked(list)
}

// Delete removes one notice.
func (b *Inbox) Delete(id string) ([]Notice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.readLocked()
	kept := list[:0]
	for _, x := range list {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	return kept, b.writeLocked(kept)
}

// Clear removes everything.
func (b *Inbox) Clear() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writeLocked([]Notice{})
}
