package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ThreadCache keeps a copy of every thread this ClawHQ has looked at, so a thread
// opens from disk at once and the gateway is only asked for what is newer. It is a
// cache, not a record: the gateway keeps the truth, and dropping the file costs
// nothing but the next load.
//
// One row per thread holds the tail as one JSON blob. The frontend merges by message
// id, so rows are replaced whole rather than appended to.
type ThreadCache struct {
	db *sql.DB
}

// CachedThread is one stored thread.
type CachedThread struct {
	GatewayID   string `json:"gatewayId"`
	Key         string `json:"key"`
	JSON        string `json:"json"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
	Full        bool   `json:"full"`
	Count       int    `json:"count"`
	StoredAtMs  int64  `json:"storedAtMs"`
}

// CacheStamp is what the frontend needs to decide whether a fetch is due.
type CacheStamp struct {
	Key         string `json:"key"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
	Count       int    `json:"count"`
	Full        bool   `json:"full"`
}

// NewThreadCache opens ~/.openclaw/clawhq-cache.sqlite, creating it if needed.
func NewThreadCache() (*ThreadCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home directory: %w", err)
	}
	dir := filepath.Join(home, ".openclaw")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "clawhq-cache.sqlite")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS threads (
		gateway_id TEXT NOT NULL,
		key TEXT NOT NULL,
		json TEXT NOT NULL,
		updated_at_ms INTEGER NOT NULL,
		full INTEGER NOT NULL DEFAULT 0,
		count INTEGER NOT NULL DEFAULT 0,
		stored_at_ms INTEGER NOT NULL,
		PRIMARY KEY (gateway_id, key)
	)`); err != nil {
		db.Close()
		return nil, err
	}
	return &ThreadCache{db: db}, nil
}

// Get returns the stored thread, or nil when there is none.
func (c *ThreadCache) Get(gatewayID, key string) (*CachedThread, error) {
	row := c.db.QueryRow(`SELECT json, updated_at_ms, full, count, stored_at_ms FROM threads WHERE gateway_id = ? AND key = ?`, gatewayID, key)
	t := CachedThread{GatewayID: gatewayID, Key: key}
	var full int
	if err := row.Scan(&t.JSON, &t.UpdatedAtMs, &full, &t.Count, &t.StoredAtMs); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	t.Full = full == 1
	return &t, nil
}

// Put replaces the stored thread.
func (c *ThreadCache) Put(t CachedThread) error {
	full := 0
	if t.Full {
		full = 1
	}
	_, err := c.db.Exec(`INSERT INTO threads (gateway_id, key, json, updated_at_ms, full, count, stored_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(gateway_id, key) DO UPDATE SET json = excluded.json, updated_at_ms = excluded.updated_at_ms,
		full = excluded.full, count = excluded.count, stored_at_ms = excluded.stored_at_ms`,
		t.GatewayID, t.Key, t.JSON, t.UpdatedAtMs, full, t.Count, time.Now().UnixMilli())
	return err
}

// Stamps lists what is cached for a gateway, without the message bodies.
func (c *ThreadCache) Stamps(gatewayID string) ([]CacheStamp, error) {
	rows, err := c.db.Query(`SELECT key, updated_at_ms, count, full FROM threads WHERE gateway_id = ?`, gatewayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CacheStamp{}
	for rows.Next() {
		var s CacheStamp
		var full int
		if err := rows.Scan(&s.Key, &s.UpdatedAtMs, &s.Count, &full); err != nil {
			return nil, err
		}
		s.Full = full == 1
		out = append(out, s)
	}
	return out, rows.Err()
}

// Clear drops every cached thread.
func (c *ThreadCache) Clear() error {
	_, err := c.db.Exec(`DELETE FROM threads`)
	return err
}

// Size is the cache file's size in bytes, for the settings page.
func (c *ThreadCache) Size() int64 {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	st, err := os.Stat(filepath.Join(home, ".openclaw", "clawhq-cache.sqlite"))
	if err != nil {
		return 0
	}
	return st.Size()
}
