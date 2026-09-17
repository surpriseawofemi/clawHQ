package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/sshx"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"
)

// ClaudeService drives Claude Code on a server without a terminal: one headless
// process per message over SSH (claude -p, streaming JSON, --resume), the records
// forwarded to the page as events, the session id kept so the next message
// continues the same conversation. History comes from Claude Code's own session
// file on the server, so the chat shows what the terminal would.
type ClaudeService struct {
	store  *store.Store
	files  *FileService
	app    *application.App
	notify *notifier
	mu     sync.Mutex
	runs   map[string]*claudeRun
}

type claudeRun struct {
	id      string
	client  *ssh.Client
	session *ssh.Session
}

// ClaudeEvent is what the page receives, one per stream-json record that matters.
type ClaudeEvent struct {
	ServerID  string          `json:"serverId"`
	RunID     string          `json:"runId"`
	Type      string          `json:"type"` // init | delta | assistant | user | result | error | done
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// ClaudeBlock is one piece of a stored message.
type ClaudeBlock struct {
	Type    string          `json:"type"` // text | tool_use | tool_result
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"isError,omitempty"`
}

type ClaudeMsg struct {
	Role   string        `json:"role"`
	AtMs   int64         `json:"atMs"`
	Blocks []ClaudeBlock `json:"blocks"`
}

type ClaudeSession struct {
	ID     string `json:"id"`
	ModMs  int64  `json:"modMs"`
	Size   int64  `json:"size"`
	Prompt string `json:"prompt"`
}

type ClaudeState struct {
	Mode      string `json:"mode"`
	SessionID string `json:"sessionId"`
	Running   bool   `json:"running"`
	Dir       string `json:"dir"`
}

func (s *ClaudeService) emit(e ClaudeEvent) {
	if s.app != nil {
		s.app.Event.Emit("claude:event", e)
	}
}

func (s *ClaudeService) profile(id string) (store.ServerProfile, error) {
	for _, p := range s.store.Read().Servers {
		if p.ID == id {
			return p, nil
		}
	}
	return store.ServerProfile{}, fmt.Errorf("unknown server %q", id)
}

func modeOf(p store.ServerProfile) string {
	switch p.ClaudeMode {
	case "auto", "manual":
		return p.ClaudeMode
	}
	return "semi"
}

// State is what the chat tab needs to draw itself.
func (s *ClaudeService) State(id string) (ClaudeState, error) {
	p, err := s.profile(id)
	if err != nil {
		return ClaudeState{}, err
	}
	s.mu.Lock()
	_, running := s.runs[id]
	s.mu.Unlock()
	return ClaudeState{Mode: modeOf(p), SessionID: p.ClaudeSessionID, Running: running, Dir: p.Dir}, nil
}

func (s *ClaudeService) SetMode(id, mode string) (ClaudeState, error) {
	switch mode {
	case "auto", "semi", "manual":
	default:
		return ClaudeState{}, fmt.Errorf("mode must be auto, semi or manual")
	}
	if _, err := s.store.SetServerClaude(id, mode, "", true); err != nil {
		return ClaudeState{}, err
	}
	return s.State(id)
}

// SetSession picks a Claude Code session to continue; empty starts a new one.
func (s *ClaudeService) SetSession(id, sessionID string) (ClaudeState, error) {
	if _, err := s.store.SetServerClaude(id, "", strings.TrimSpace(sessionID), false); err != nil {
		return ClaudeState{}, err
	}
	return s.State(id)
}

// Safe read-only commands the semi mode lets Claude Code run without asking.
var semiAllowedTools = []string{
	"Read", "Glob", "Grep", "LS", "Edit", "Write", "MultiEdit", "NotebookEdit", "WebFetch", "WebSearch", "TodoWrite", "Task",
	"Bash(git status:*)", "Bash(git log:*)", "Bash(git diff:*)", "Bash(git show:*)", "Bash(git branch:*)",
	"Bash(ls:*)", "Bash(cat:*)", "Bash(head:*)", "Bash(tail:*)", "Bash(grep:*)", "Bash(rg:*)", "Bash(find:*)", "Bash(wc:*)",
	"Bash(pwd)", "Bash(whoami)", "Bash(uname:*)", "Bash(df:*)", "Bash(du:*)", "Bash(free:*)", "Bash(ps:*)", "Bash(env)",
	"Bash(node --version)", "Bash(npm --version)", "Bash(python --version)", "Bash(python3 --version)",
	"Bash(npm test:*)", "Bash(npm run:*)", "Bash(pnpm test:*)", "Bash(pytest:*)", "Bash(go test:*)", "Bash(go build:*)", "Bash(go vet:*)",
	"Bash(make:*)", "Bash(docker ps:*)", "Bash(docker logs:*)", "Bash(systemctl status:*)", "Bash(journalctl:*)",
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Send starts one headless turn. Records stream back as claude:event.
func (s *ClaudeService) Send(ctx context.Context, id, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("nothing to send")
	}
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if _, busy := s.runs[id]; busy {
		s.mu.Unlock()
		return "", fmt.Errorf("Claude Code is still working on the previous message")
	}
	s.mu.Unlock()

	client, err := sshx.Dial(ctx, sshx.Target{Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, Password: p.Password}, knownHostsPath())
	if err != nil {
		return "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return "", err
	}
	args := []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if p.ClaudeSessionID != "" {
		args = append(args, "--resume", p.ClaudeSessionID)
	}
	switch modeOf(p) {
	case "auto":
		args = append(args, "--permission-mode", "bypassPermissions", "--dangerously-skip-permissions")
	case "semi":
		args = append(args, "--permission-mode", "acceptEdits", "--allowedTools")
		args = append(args, semiAllowedTools...)
	default:
		args = append(args, "--permission-mode", "default")
	}
	var b strings.Builder
	if p.Dir != "" {
		b.WriteString("cd " + shq(p.Dir) + " && ")
	}
	b.WriteString(`export PATH="$HOME/.local/bin:$PATH"; `)
	for i, a := range args {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(shq(a))
	}
	cmd := "bash -lc " + shq(b.String())

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	var stderr strings.Builder
	sess.Stderr = &stderr
	if err := sess.Start(cmd); err != nil {
		sess.Close()
		client.Close()
		return "", err
	}
	runID := fmt.Sprintf("cc-%d", time.Now().UnixMilli())
	s.mu.Lock()
	if s.runs == nil {
		s.runs = map[string]*claudeRun{}
	}
	s.runs[id] = &claudeRun{id: runID, client: client, session: sess}
	s.mu.Unlock()

	go func() {
		_, _ = io.WriteString(stdin, text)
		_ = stdin.Close()
	}()
	go s.pump(id, runID, p, stdout, sess, client, &stderr)
	return runID, nil
}

func (s *ClaudeService) pump(id, runID string, p store.ServerProfile, stdout io.Reader, sess *ssh.Session, client *ssh.Client, stderr *strings.Builder) {
	defer func() {
		s.mu.Lock()
		if r, ok := s.runs[id]; ok && r.id == runID {
			delete(s.runs, id)
		}
		s.mu.Unlock()
		sess.Close()
		client.Close()
		s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "done"})
	}()
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 32<<20)
	sessionID := ""
	finalText := ""
	gotResult := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] != '{' {
			continue
		}
		var rec struct {
			Type      string          `json:"type"`
			Subtype   string          `json:"subtype"`
			SessionID string          `json:"session_id"`
			Event     json.RawMessage `json:"event"`
			Message   json.RawMessage `json:"message"`
			Result    string          `json:"result"`
			IsError   bool            `json:"is_error"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec.SessionID != "" && sessionID == "" {
			sessionID = rec.SessionID
			_, _ = s.store.SetServerClaude(id, "", sessionID, false)
		}
		switch rec.Type {
		case "system":
			if rec.Subtype == "init" {
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "init", SessionID: sessionID})
			}
		case "stream_event":
			var ev struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(rec.Event, &ev) == nil && ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "delta", Text: ev.Delta.Text})
			}
		case "assistant", "user":
			s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: rec.Type, Message: rec.Message})
		case "result":
			gotResult = true
			finalText = rec.Result
			s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "result", SessionID: sessionID, Text: rec.Result, Result: json.RawMessage(line)})
		}
	}
	err := sess.Wait()
	if !gotResult {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if msg == "" {
			msg = "Claude Code ended without a result"
		}
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "error", Text: msg})
		log.Printf("claude: %s: %s", p.Name, msg)
		return
	}
	if s.notify != nil {
		body := strings.TrimSpace(finalText)
		if len(body) > 240 {
			body = body[:240] + "…"
		}
		if body == "" {
			body = "Finished."
		}
		s.notify.show(store.Notice{Title: "Claude Code · " + p.Name, Body: body, AgentName: "Claude Code", AgentEmoji: "🧑‍💻", Origin: "server:" + p.ID, AtMs: time.Now().UnixMilli()})
	}
}

// Abort stops the running turn.
func (s *ClaudeService) Abort(id string) error {
	s.mu.Lock()
	r, ok := s.runs[id]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	_ = r.session.Signal(ssh.SIGINT)
	time.AfterFunc(2*time.Second, func() {
		_ = r.session.Close()
		_ = r.client.Close()
	})
	return nil
}

var projectDirRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// projectDir is where Claude Code keeps a folder's sessions: ~/.claude/projects/<dir with every other character turned into "-">.
func projectDir(home, dir string) string {
	return path.Join(home, ".claude", "projects", projectDirRe.ReplaceAllString(dir, "-"))
}

// Sessions lists Claude Code sessions for the server's project folder, newest first.
func (s *ClaudeService) Sessions(ctx context.Context, id string) ([]ClaudeSession, error) {
	p, err := s.profile(id)
	if err != nil {
		return nil, err
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	home, _ := c.sftp.Getwd()
	dir := p.Dir
	if dir == "" {
		dir = home
	}
	pdir := projectDir(home, dir)
	infos, err := c.sftp.ReadDir(pdir)
	if err != nil {
		return []ClaudeSession{}, nil // no sessions yet
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ModTime().After(infos[j].ModTime()) })
	out := []ClaudeSession{}
	for _, fi := range infos {
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".jsonl") {
			continue
		}
		cs := ClaudeSession{ID: strings.TrimSuffix(fi.Name(), ".jsonl"), ModMs: fi.ModTime().UnixMilli(), Size: fi.Size()}
		if len(out) < 40 {
			if f, err := c.sftp.Open(path.Join(pdir, fi.Name())); err == nil {
				head := make([]byte, 16384)
				n, _ := io.ReadFull(f, head)
				f.Close()
				cs.Prompt = firstPrompt(head[:n])
			}
		}
		if cs.Prompt == "" {
			cs.Prompt = "(no message yet)"
		}
		out = append(out, cs)
	}
	return out, nil
}

func firstPrompt(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		var rec struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil || rec.Type != "user" || rec.IsMeta {
			continue
		}
		var str string
		if json.Unmarshal(rec.Message.Content, &str) == nil {
			return firstLine(str)
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(rec.Message.Content, &blocks) == nil {
			for _, bl := range blocks {
				if bl.Type == "text" && strings.TrimSpace(bl.Text) != "" {
					return firstLine(bl.Text)
				}
			}
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// History reads the current session's file from the server.
func (s *ClaudeService) History(ctx context.Context, id string) ([]ClaudeMsg, error) {
	p, err := s.profile(id)
	if err != nil {
		return nil, err
	}
	if p.ClaudeSessionID == "" {
		return []ClaudeMsg{}, nil
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	home, _ := c.sftp.Getwd()
	dir := p.Dir
	if dir == "" {
		dir = home
	}
	f, err := c.sftp.Open(path.Join(projectDir(home, dir), p.ClaudeSessionID+".jsonl"))
	if err != nil {
		return []ClaudeMsg{}, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	out := []ClaudeMsg{}
	for sc.Scan() {
		var rec struct {
			Type        string `json:"type"`
			IsMeta      bool   `json:"isMeta"`
			IsSidechain bool   `json:"isSidechain"`
			Timestamp   string `json:"timestamp"`
			Message     struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil || (rec.Type != "user" && rec.Type != "assistant") || rec.IsMeta || rec.IsSidechain {
			continue
		}
		m := ClaudeMsg{Role: rec.Type}
		if t, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
			m.AtMs = t.UnixMilli()
		}
		m.Blocks = parseBlocks(rec.Message.Content)
		if len(m.Blocks) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// parseBlocks turns a message's content (string or blocks) into ClaudeBlocks.
func parseBlocks(raw json.RawMessage) []ClaudeBlock {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		if strings.TrimSpace(str) == "" {
			return nil
		}
		return []ClaudeBlock{{Type: "text", Text: str}}
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Name    string          `json:"name"`
		Input   json.RawMessage `json:"input"`
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"is_error"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	out := []ClaudeBlock{}
	for _, bl := range blocks {
		switch bl.Type {
		case "text":
			if strings.TrimSpace(bl.Text) != "" {
				out = append(out, ClaudeBlock{Type: "text", Text: bl.Text})
			}
		case "tool_use":
			out = append(out, ClaudeBlock{Type: "tool_use", Name: bl.Name, Input: bl.Input})
		case "tool_result":
			txt := ""
			var cs string
			if json.Unmarshal(bl.Content, &cs) == nil {
				txt = cs
			} else {
				var parts []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if json.Unmarshal(bl.Content, &parts) == nil {
					var sb strings.Builder
					for _, pt := range parts {
						if pt.Type == "text" {
							sb.WriteString(pt.Text)
							sb.WriteString("\n")
						}
					}
					txt = sb.String()
				}
			}
			if len(txt) > 4000 {
				txt = txt[:4000] + "\n…"
			}
			out = append(out, ClaudeBlock{Type: "tool_result", Text: strings.TrimSpace(txt), IsError: bl.IsError})
		}
	}
	return out
}
