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
	runlog *store.RunLog
	mu     sync.Mutex
	runs   map[string]*claudeRun
}

type claudeRun struct {
	id      string
	client  *ssh.Client
	session *ssh.Session
	// done receives the outcome once, for callers that wait (server tasks).
	done chan runOutcome
}

type runOutcome struct {
	Text    string
	CostUsd float64
	OK      bool
	Err     string
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
	Agent     string `json:"agent"`
	Mode      string `json:"mode"`
	SessionID string `json:"sessionId"`
	Running   bool   `json:"running"`
	Dir       string `json:"dir"`
	ProjectID string `json:"projectId"`
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

func modeOfProject(pr store.ServerProject) string {
	switch pr.Mode {
	case "semi", "manual":
		return pr.Mode
	}
	return "auto"
}

func agentOfProject(pr store.ServerProject) string {
	switch pr.Agent {
	case "codex", "gemini", "grok", "opencode":
		return pr.Agent
	}
	return "claude"
}

// State is what the chat tab needs to draw itself, for the server's active project.
func (s *ClaudeService) State(id string) (ClaudeState, error) {
	p, err := s.profile(id)
	if err != nil {
		return ClaudeState{}, err
	}
	pr := p.Active()
	s.mu.Lock()
	_, running := s.runs[id]
	s.mu.Unlock()
	agent := agentOfProject(pr)
	return ClaudeState{Agent: agent, Mode: modeOfProject(pr), SessionID: pr.Sessions[agent], Running: running, Dir: pr.Dir, ProjectID: pr.ID}, nil
}

func (s *ClaudeService) SetMode(id, mode string) (ClaudeState, error) {
	switch mode {
	case "auto", "semi", "manual":
	default:
		return ClaudeState{}, fmt.Errorf("mode must be auto, semi or manual")
	}
	if _, err := s.store.UpdateProject(id, "", func(pr *store.ServerProject) { pr.Mode = mode }); err != nil {
		return ClaudeState{}, err
	}
	return s.State(id)
}

// SetSession picks a session to continue for the active project's agent; empty starts a new one.
func (s *ClaudeService) SetSession(id, sessionID string) (ClaudeState, error) {
	sessionID = strings.TrimSpace(sessionID)
	if _, err := s.store.UpdateProject(id, "", func(pr *store.ServerProject) {
		if pr.Sessions == nil {
			pr.Sessions = map[string]string{}
		}
		pr.Sessions[agentOfProject(*pr)] = sessionID
	}); err != nil {
		return ClaudeState{}, err
	}
	return s.State(id)
}

// SetAgent picks which coding agent the active project's chat drives.
func (s *ClaudeService) SetAgent(id, agent string) (ClaudeState, error) {
	switch agent {
	case "claude", "codex", "gemini", "grok", "opencode":
	default:
		return ClaudeState{}, fmt.Errorf("unknown agent %q", agent)
	}
	if _, err := s.store.UpdateProject(id, "", func(pr *store.ServerProject) { pr.Agent = agent }); err != nil {
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

// agentCommand builds the headless command for one turn. The prompt always goes
// through stdin, so it never touches the shell.
func agentCommand(agent, sessionID, mode string) []string {
	switch agent {
	case "codex":
		// codex exec reads the prompt from stdin with "-"; resume keeps the thread.
		args := []string{"codex", "exec", "--json", "--skip-git-repo-check"}
		switch mode {
		case "auto":
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		case "semi":
			args = append(args, "--full-auto")
		default:
			args = append(args, "--sandbox", "read-only")
		}
		if sessionID != "" {
			args = append(args, "resume", sessionID, "-")
		} else {
			args = append(args, "-")
		}
		return args
	case "gemini":
		args := []string{"gemini", "--output-format", "stream-json"}
		switch mode {
		case "auto":
			args = append(args, "--yolo")
		case "semi":
			args = append(args, "--approval-mode", "auto_edit")
		}
		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}
		return args
	case "grok":
		// xAI's Grok CLI speaks the same streaming format as Claude Code and
		// resumes by session id; the prompt goes in as an argument (see Send).
		args := []string{"grok", "-p", "__PROMPT__", "--output-format", "streaming-messages-json", "--include-partial-messages"}
		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}
		switch mode {
		case "auto":
			args = append(args, "--permission-mode", "bypassPermissions")
		case "semi":
			args = append(args, "--permission-mode", "acceptEdits")
		default:
			args = append(args, "--permission-mode", "default")
		}
		return args
	case "opencode":
		// opencode run reads the prompt from stdin and prints one JSON event per
		// line; --auto approves whatever its config does not deny, and the built-in
		// plan agent is read-only.
		args := []string{"opencode", "run", "--format", "json"}
		switch mode {
		case "auto":
			args = append(args, "--auto")
		case "manual":
			args = append(args, "--agent", "plan")
		}
		if sessionID != "" {
			args = append(args, "--session", sessionID)
		}
		return args
	default:
		args := []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
		if sessionID != "" {
			args = append(args, "--resume", sessionID)
		}
		switch mode {
		case "auto":
			args = append(args, "--permission-mode", "bypassPermissions", "--dangerously-skip-permissions")
		case "semi":
			args = append(args, "--permission-mode", "acceptEdits", "--allowedTools")
			args = append(args, semiAllowedTools...)
		default:
			args = append(args, "--permission-mode", "default")
		}
		return args
	}
}

// Send starts one headless turn for the active project. Records stream back as claude:event.
func (s *ClaudeService) Send(ctx context.Context, id, text string) (string, error) {
	_, runID, err := s.sendProject(ctx, id, "", text, "chat")
	return runID, err
}

// sendProject starts a turn in a given project (empty: the active one) and returns
// the run so a caller can wait on it.
func (s *ClaudeService) sendProject(ctx context.Context, id, projectID, text, source string) (*claudeRun, string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, "", fmt.Errorf("nothing to send")
	}
	p, err := s.profile(id)
	if err != nil {
		return nil, "", err
	}
	pr := p.Active()
	if projectID != "" {
		found := false
		for _, x := range p.Projects {
			if x.ID == projectID {
				pr, found = x, true
			}
		}
		if !found {
			return nil, "", fmt.Errorf("no project %q on %s", projectID, p.Name)
		}
	}
	agent := agentOfProject(pr)
	s.mu.Lock()
	if _, busy := s.runs[id]; busy {
		s.mu.Unlock()
		return nil, "", fmt.Errorf("%s is still working on the previous message", agentLabel(agent))
	}
	s.mu.Unlock()

	client, err := sshx.Dial(ctx, sshx.Target{Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, Password: p.Password}, knownHostsPath())
	if err != nil {
		return nil, "", err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, "", err
	}
	args := agentCommand(agent, pr.Sessions[agent], modeOfProject(pr))
	for i := range args {
		if args[i] == "__PROMPT__" {
			args[i] = text
		}
	}
	var cmd string
	if p.Platform == "windows" {
		var b strings.Builder
		if pr.Dir != "" {
			b.WriteString("Set-Location -LiteralPath " + psq(pr.Dir) + "; ")
		}
		b.WriteString("$input | & ")
		for i, a := range args {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(psq(a))
		}
		cmd = psCommand(b.String())
	} else {
		var b strings.Builder
		if pr.Dir != "" {
			b.WriteString("cd " + shq(pr.Dir) + " && ")
		}
		b.WriteString(`export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:$HOME/.grok/bin:$HOME/.opencode/bin:$PATH"; `)
		for i, a := range args {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(shq(a))
		}
		cmd = "bash -lc " + shq(b.String())
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, "", err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, "", err
	}
	var stderr strings.Builder
	sess.Stderr = &stderr
	if err := sess.Start(cmd); err != nil {
		sess.Close()
		client.Close()
		return nil, "", err
	}
	runID := fmt.Sprintf("cc-%d", time.Now().UnixMilli())
	run := &claudeRun{id: runID, client: client, session: sess, done: make(chan runOutcome, 1)}
	s.mu.Lock()
	if s.runs == nil {
		s.runs = map[string]*claudeRun{}
	}
	s.runs[id] = run
	s.mu.Unlock()

	go func() {
		if agent != "grok" {
			_, _ = io.WriteString(stdin, text)
		}
		if agent == "gemini" || agent == "opencode" {
			_, _ = io.WriteString(stdin, "\n")
		}
		_ = stdin.Close()
	}()
	go s.pump(id, run, p, pr, agent, source, stdout, sess, client, &stderr)
	return run, runID, nil
}

func agentLabel(agent string) string {
	switch agent {
	case "codex":
		return "Codex"
	case "gemini":
		return "Gemini CLI"
	case "grok":
		return "Grok CLI"
	case "opencode":
		return "OpenCode"
	}
	return "Claude Code"
}

func (s *ClaudeService) pump(id string, run *claudeRun, p store.ServerProfile, pr store.ServerProject, agent, source string, stdout io.Reader, sess *ssh.Session, client *ssh.Client, stderr *strings.Builder) {
	runID := run.id
	started := time.Now()
	outcome := runOutcome{}
	defer func() {
		s.mu.Lock()
		if r, ok := s.runs[id]; ok && r.id == runID {
			delete(s.runs, id)
		}
		s.mu.Unlock()
		sess.Close()
		client.Close()
		s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "done"})
		run.done <- outcome
	}()
	var costUsd float64
	turns := 0
	sessionID := ""
	remember := func(sid string) {
		if sid == "" || sessionID != "" {
			return
		}
		sessionID = sid
		_, _ = s.store.UpdateProject(id, pr.ID, func(x *store.ServerProject) {
			if x.Sessions == nil {
				x.Sessions = map[string]string{}
			}
			x.Sessions[agent] = sid
		})
	}
	var finalText strings.Builder
	gotResult := false

	switch agent {
	case "claude", "grok":
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 32<<20)
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
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue
			}
			remember(rec.SessionID)
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
				finalText.Reset()
				finalText.WriteString(rec.Result)
				var meta struct {
					Cost  float64 `json:"total_cost_usd"`
					Turns int     `json:"num_turns"`
				}
				_ = json.Unmarshal([]byte(line), &meta)
				costUsd, turns = meta.Cost, meta.Turns
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "result", SessionID: sessionID, Text: rec.Result, Result: json.RawMessage(line)})
			}
		}
	case "codex":
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 32<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || line[0] != '{' {
				continue
			}
			var rec struct {
				Type     string `json:"type"`
				ThreadID string `json:"thread_id"`
				Item     struct {
					Type             string `json:"type"`
					Text             string `json:"text"`
					Command          string `json:"command"`
					AggregatedOutput string `json:"aggregated_output"`
					ExitCode         *int   `json:"exit_code"`
				} `json:"item"`
				Usage   json.RawMessage `json:"usage"`
				Message string          `json:"message"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil {
				continue
			}
			switch rec.Type {
			case "thread.started":
				remember(rec.ThreadID)
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "init", SessionID: sessionID})
			case "item.completed":
				switch rec.Item.Type {
				case "agent_message":
					finalText.Reset()
					finalText.WriteString(rec.Item.Text)
					s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "delta", Text: rec.Item.Text + "\n\n"})
				case "command_execution":
					msg, _ := json.Marshal(map[string]any{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": rec.Item.Command}}}})
					s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "assistant", Message: msg})
					res, _ := json.Marshal(map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result", "content": rec.Item.AggregatedOutput, "is_error": rec.Item.ExitCode != nil && *rec.Item.ExitCode != 0}}})
					s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "user", Message: res})
				}
			case "turn.completed":
				gotResult = true
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "result", SessionID: sessionID, Text: finalText.String(), Result: json.RawMessage(line)})
			case "error":
				stderr.WriteString(rec.Message)
			}
		}
	case "opencode":
		// One event per line: text (final text of a part), tool_use (completed or
		// failed), step_start/step_finish (tokens and cost), error. No deltas.
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 32<<20)
		steps := 0
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || line[0] != '{' {
				continue
			}
			var rec struct {
				Type      string          `json:"type"`
				SessionID string          `json:"sessionID"`
				Part      json.RawMessage `json:"part"`
				Error     json.RawMessage `json:"error"`
			}
			if json.Unmarshal([]byte(line), &rec) != nil {
				continue
			}
			if sessionID == "" && rec.SessionID != "" {
				remember(rec.SessionID)
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "init", SessionID: sessionID})
			}
			switch rec.Type {
			case "text":
				var part struct {
					Text string `json:"text"`
				}
				if json.Unmarshal(rec.Part, &part) != nil || part.Text == "" {
					continue
				}
				if finalText.Len() > 0 {
					finalText.WriteString("\n\n")
				}
				finalText.WriteString(part.Text)
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "delta", Text: part.Text + "\n\n"})
			case "tool_use":
				var part struct {
					Tool  string `json:"tool"`
					State struct {
						Status string          `json:"status"`
						Input  json.RawMessage `json:"input"`
						Output string          `json:"output"`
						Error  string          `json:"error"`
					} `json:"state"`
				}
				if json.Unmarshal(rec.Part, &part) != nil {
					continue
				}
				input := part.State.Input
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				msg, _ := json.Marshal(map[string]any{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "name": part.Tool, "input": input}}})
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "assistant", Message: msg})
				out := part.State.Output
				if part.State.Status == "error" && part.State.Error != "" {
					out = part.State.Error
				}
				res, _ := json.Marshal(map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result", "content": out, "is_error": part.State.Status == "error"}}})
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "user", Message: res})
			case "step_finish":
				var part struct {
					Cost float64 `json:"cost"`
				}
				_ = json.Unmarshal(rec.Part, &part)
				costUsd += part.Cost
				steps++
			case "error":
				stderr.WriteString(string(rec.Error))
			}
		}
		if steps > 0 || finalText.Len() > 0 {
			gotResult = true
			turns = steps
			result, _ := json.Marshal(map[string]any{"type": "result", "total_cost_usd": costUsd, "num_turns": turns, "duration_ms": time.Since(started).Milliseconds()})
			s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "result", SessionID: sessionID, Text: finalText.String(), Result: json.RawMessage(result)})
		}
	default: // gemini: text stream; one chunk at a time
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := string(buf[:n])
				finalText.WriteString(chunk)
				s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "delta", Text: chunk})
			}
			if err != nil {
				break
			}
		}
		if finalText.Len() > 0 {
			gotResult = true
			s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "result", Text: finalText.String(), Result: json.RawMessage(`{"type":"result"}`)})
		}
	}
	err := sess.Wait()
	if !gotResult {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if msg == "" {
			msg = agentLabel(agent) + " ended without a result"
		}
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		s.emit(ClaudeEvent{ServerID: id, RunID: runID, Type: "error", Text: msg})
		log.Printf("agents: %s: %s", p.Name, msg)
		outcome = runOutcome{OK: false, Err: msg, Text: finalText.String()}
		if s.runlog != nil {
			s.runlog.Append(store.ServerRun{ServerID: p.ID, ServerName: p.Name, ProjectID: pr.ID, Project: pr.Name, Agent: agent, AtMs: started.UnixMilli(), DurationMs: time.Since(started).Milliseconds(), OK: false, Summary: firstLine(msg), Source: source})
		}
		return
	}
	outcome = runOutcome{OK: true, Text: finalText.String(), CostUsd: costUsd}
	if s.runlog != nil {
		s.runlog.Append(store.ServerRun{ServerID: p.ID, ServerName: p.Name, ProjectID: pr.ID, Project: pr.Name, Agent: agent, AtMs: started.UnixMilli(), DurationMs: time.Since(started).Milliseconds(), CostUsd: costUsd, Turns: turns, OK: true, Summary: firstLine(finalText.String()), Source: source})
	}
	if source == "task" {
		return // the bell rings when the task's result reaches the board
	}
	if s.notify != nil {
		body := strings.TrimSpace(finalText.String())
		if len(body) > 240 {
			body = body[:240] + "…"
		}
		if body == "" {
			body = "Finished."
		}
		s.notify.show(store.Notice{Title: agentLabel(agent) + " · " + p.Name + " · " + pr.Name, Body: body, AgentName: agentLabel(agent), AgentEmoji: "🧑‍💻", Origin: "server:" + p.ID, AtMs: time.Now().UnixMilli()})
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
	pr := p.Active()
	if agentOfProject(pr) != "claude" {
		return []ClaudeSession{}, nil
	}
	home, _ := c.sftp.Getwd()
	dir := pr.Dir
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
	pr := p.Active()
	sid := pr.Sessions["claude"]
	if agentOfProject(pr) != "claude" || sid == "" {
		return []ClaudeMsg{}, nil
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	home, _ := c.sftp.Getwd()
	dir := pr.Dir
	if dir == "" {
		dir = home
	}
	f, err := c.sftp.Open(path.Join(projectDir(home, dir), sid+".jsonl"))
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

// RunTask runs one turn in a project and waits for the outcome, queueing behind a
// run already in flight on that server for up to ten minutes. Used for tasks agents
// hand to servers through the plugin.
func (s *ClaudeService) RunTask(ctx context.Context, id, projectID, text string) (runOutcome, error) {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		run, _, err := s.sendProject(ctx, id, projectID, text, "task")
		if err == nil {
			select {
			case out := <-run.done:
				return out, nil
			case <-ctx.Done():
				_ = s.Abort(id)
				return runOutcome{}, ctx.Err()
			}
		}
		if !strings.Contains(err.Error(), "still working") || time.Now().After(deadline) {
			return runOutcome{}, err
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return runOutcome{}, ctx.Err()
		}
	}
}

// RunsSince lists logged runs for the digest.
func (s *ClaudeService) RunsSince(atMs int64) []store.ServerRun {
	if s.runlog == nil {
		return []store.ServerRun{}
	}
	return s.runlog.Since(atMs)
}

// SessionUsage sums Claude Code token usage for a server's projects from the
// session files themselves, so terminal sessions count too. Read-only: nothing
// is sent to any model.
type SessionUsage struct {
	ServerID     string `json:"serverId"`
	Project      string `json:"project"`
	Sessions     int    `json:"sessions"`
	Messages     int    `json:"messages"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	CacheRead    int64  `json:"cacheRead"`
}

func (s *ClaudeService) UsageSince(ctx context.Context, id string, sinceMs int64) ([]SessionUsage, error) {
	p, err := s.profile(id)
	if err != nil {
		return nil, err
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	home, _ := c.sftp.Getwd()
	out := []SessionUsage{}
	since := time.UnixMilli(sinceMs)
	for _, pr := range p.Projects {
		dir := pr.Dir
		if dir == "" {
			dir = home
		}
		pdir := projectDir(home, dir)
		infos, err := c.sftp.ReadDir(pdir)
		if err != nil {
			continue
		}
		u := SessionUsage{ServerID: id, Project: pr.Name}
		for _, fi := range infos {
			if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".jsonl") || fi.ModTime().Before(since) {
				continue
			}
			f, err := c.sftp.Open(path.Join(pdir, fi.Name()))
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 1<<20), 64<<20)
			counted := false
			for sc.Scan() {
				var rec struct {
					Type      string `json:"type"`
					Timestamp string `json:"timestamp"`
					Message   struct {
						Usage struct {
							Input     int64 `json:"input_tokens"`
							Output    int64 `json:"output_tokens"`
							CacheRead int64 `json:"cache_read_input_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Type != "assistant" {
					continue
				}
				t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
				if err != nil || t.Before(since) {
					continue
				}
				u.Messages++
				u.InputTokens += rec.Message.Usage.Input
				u.OutputTokens += rec.Message.Usage.Output
				u.CacheRead += rec.Message.Usage.CacheRead
				counted = true
			}
			f.Close()
			if counted {
				u.Sessions++
			}
		}
		if u.Sessions > 0 {
			out = append(out, u)
		}
	}
	return out, nil
}
