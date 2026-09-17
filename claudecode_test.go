package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectDir(t *testing.T) {
	got := projectDir("/home/agent", "/var/www/app.example")
	if got != "/home/agent/.claude/projects/-var-www-app-example" {
		t.Fatalf("got %s", got)
	}
	// Matches what Claude Code produced for this very checkout on the Mac.
	home, _ := os.UserHomeDir()
	local := projectDir(home, "/Users/superuser/Library/.systems/AMI Projects/clawHQ")
	if _, err := os.Stat(local); err != nil {
		t.Skipf("no local session folder at %s", local)
	}
}

func TestParseBlocksAndFirstPrompt(t *testing.T) {
	home, _ := os.UserHomeDir()
	dir := projectDir(home, "/Users/superuser/Library/.systems/AMI Projects/clawHQ")
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(files) == 0 {
		t.Skip("no local session files")
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	head := b
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	if p := firstPrompt(head); p == "" {
		t.Fatalf("no first prompt found in %s", files[0])
	}
	n := 0
	for _, line := range strings.Split(string(head), "\n") {
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil || (rec.Type != "user" && rec.Type != "assistant") {
			continue
		}
		n += len(parseBlocks(rec.Message.Content))
	}
	if n == 0 {
		t.Fatal("parsed no blocks")
	}
}
