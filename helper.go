package main

import (
	"bufio"
	"context"
	_ "embed"
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

	"github.com/pkg/sftp"
	"github.com/surpriseawofemi/clawhq/internal/sshx"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed serverhelper/helper.js
var helperJS string

// HelperService installs and talks to the ClawHQ helper on a server: one Node
// file uploaded over SFTP that gives Claude Code hooks (every reply lands in an
// outbox ClawHQ reads) and MCP tools (short log lines, numbered issues, the
// mission). Nothing is fetched from a registry; the helper's version is ClawHQ's.
type HelperService struct {
	store   *store.Store
	files   *FileService
	servers *ServerService
	notify  *notifier
	app     *application.App
	version string
	mu      sync.Mutex
	// offsets remembers how far into each server's outbox the watcher has read.
	offsets map[string]int64
	present map[string]bool
}

type HelperStatus struct {
	Installed bool            `json:"installed"`
	Version   string          `json:"version"`
	Current   bool            `json:"current"`
	Projects  []ProjectWiring `json:"projects"`
}

type ProjectWiring struct {
	ProjectID  string `json:"projectId"`
	Dir        string `json:"dir"`
	MCP        bool   `json:"mcp"`
	Hooks      bool   `json:"hooks"`
	ClaudeMd   bool   `json:"claudeMd"`
	Mission    bool   `json:"mission"`
	Wired      bool   `json:"wired"`
	OpenIssues int    `json:"openIssues"`
}

// OutboxEvent is one line of ~/.clawhq/outbox.jsonl.
type OutboxEvent struct {
	Ts        int64           `json:"ts"`
	Type      string          `json:"type"`
	Event     string          `json:"event,omitempty"`
	Project   string          `json:"project"`
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Kind      string          `json:"kind,omitempty"`
	Model     string          `json:"model,omitempty"`
	Usage     json.RawMessage `json:"usage,omitempty"`
	Files     []string        `json:"files,omitempty"`
	Billing   string          `json:"billing,omitempty"`
	N         int             `json:"n,omitempty"`
	Title     string          `json:"title,omitempty"`
	NeedsBoss bool            `json:"needsBoss,omitempty"`
	Urgency   string          `json:"urgency,omitempty"`
	Status    string          `json:"status,omitempty"`
	Note      string          `json:"note,omitempty"`
}

type ServerIssue struct {
	N         int    `json:"n"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	NeedsBoss bool   `json:"needsBoss"`
	Urgency   string `json:"urgency"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Notes     []struct {
		Ts   int64  `json:"ts"`
		Text string `json:"text"`
	} `json:"notes"`
}

const helperRemote = ".clawhq/helper.js"

var helperVersionRe = regexp.MustCompile(`const VERSION = '([^']*)'`)

func (s *HelperService) profile(id string) (store.ServerProfile, error) {
	for _, p := range s.store.Read().Servers {
		if p.ID == id {
			return p, nil
		}
	}
	return store.ServerProfile{}, fmt.Errorf("unknown server %q", id)
}

func readRemote(c *sftp.Client, p string) (string, error) {
	f, err := c.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4<<20))
	return string(b), err
}

func writeRemote(c *sftp.Client, p, content string, mode uint32) error {
	_ = c.MkdirAll(path.Dir(p))
	tmp := p + ".clawhq-tmp"
	f, err := c.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if mode != 0 {
		_ = c.Chmod(tmp, fsMode(mode))
	}
	if err := c.PosixRename(tmp, p); err != nil {
		return c.Rename(tmp, p)
	}
	return nil
}

// Install uploads the helper for this ClawHQ version.
func (s *HelperService) Install(ctx context.Context, id string) (HelperStatus, error) {
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return HelperStatus{}, err
	}
	home, _ := c.sftp.Getwd()
	content := strings.Replace(helperJS, "__CLAWHQ_HELPER_VERSION__", s.version, 1)
	if err := writeRemote(c.sftp, path.Join(home, helperRemote), content, 0o755); err != nil {
		return HelperStatus{}, err
	}
	log.Printf("helper: installed %s on %s", s.version, id)
	s.mu.Lock()
	if s.present == nil {
		s.present = map[string]bool{}
	}
	s.present[id] = true
	s.mu.Unlock()
	return s.Status(ctx, id)
}

// Status reports the helper's version and how each project is wired.
func (s *HelperService) Status(ctx context.Context, id string) (HelperStatus, error) {
	p, err := s.profile(id)
	if err != nil {
		return HelperStatus{}, err
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return HelperStatus{}, err
	}
	home, _ := c.sftp.Getwd()
	st := HelperStatus{Projects: []ProjectWiring{}}
	if src, err := readRemote(c.sftp, path.Join(home, helperRemote)); err == nil {
		st.Installed = true
		if m := helperVersionRe.FindStringSubmatch(src); len(m) == 2 {
			st.Version = m[1]
		}
		st.Current = st.Version == s.version
	}
	s.mu.Lock()
	if s.present == nil {
		s.present = map[string]bool{}
	}
	s.present[id] = st.Installed
	s.mu.Unlock()
	helperPath := path.Join(home, helperRemote)
	for _, pr := range p.Projects {
		dir := pr.Dir
		if dir == "" {
			dir = home
		}
		w := ProjectWiring{ProjectID: pr.ID, Dir: dir}
		if raw, err := readRemote(c.sftp, path.Join(dir, ".mcp.json")); err == nil {
			w.MCP = strings.Contains(raw, helperPath)
		}
		if raw, err := readRemote(c.sftp, path.Join(dir, ".claude", "settings.json")); err == nil {
			w.Hooks = strings.Contains(raw, helperPath)
		}
		if raw, err := readRemote(c.sftp, path.Join(dir, "CLAUDE.md")); err == nil {
			w.ClaudeMd = strings.Contains(raw, "## ClawHQ")
		}
		if _, err := c.sftp.Stat(path.Join(dir, ".clawhq", "MISSION.md")); err == nil {
			w.Mission = true
		}
		if raw, err := readRemote(c.sftp, path.Join(dir, ".clawhq", "issues.json")); err == nil {
			var db struct {
				Issues []ServerIssue `json:"issues"`
			}
			if json.Unmarshal([]byte(raw), &db) == nil {
				for _, is := range db.Issues {
					if is.Status == "open" {
						w.OpenIssues++
					}
				}
			}
		}
		w.Wired = w.MCP && w.Hooks && w.ClaudeMd
		st.Projects = append(st.Projects, w)
	}
	return st, nil
}

const claudeMdSection = `

## ClawHQ

ClawHQ is the app the boss reads. It gives you five tools over MCP:
- clawhq_log: one short line per thing checked, changed or found. The boss reads these in the app; keep them to a sentence.
- clawhq_issue: anything needing the boss's decision or approval, or too risky to just do. It returns a number and a details file; write everything worth remembering into that file, so when the boss says "#12" you read it with clawhq_issues and answer from it.
- clawhq_issue_update: note progress, mark done or dismissed.
- clawhq_issues: list open issues or read one by number.
- clawhq_mission: the standing mission for this project (focus rotation, what you may change without asking, how to report). Read it at the start of every scheduled run.

Rules: refer to issues only by number (#12). Short lines in the log, long text in the details file. Never change the schema, dependencies, secrets or server config without an issue the boss has approved.
`

const missionTemplate = `# Mission

Goal: keep this application correct, fast and safe. Work continuously; log every step in one line with clawhq_log; open a numbered issue with clawhq_issue for anything that needs the boss.

## Schedule
Every hour, start a run. Pick the next focus area from the rotation below (keep your place in .clawhq/rotation.txt). Spawn sub-agents to review that area: bugs, error handling, performance, database access, security. Then act on what they found.

## Rotation
1. request handling and routing
2. database queries and indexes
3. background jobs and queues
4. email sending and receiving paths
5. authentication, sessions and permissions
6. logging, monitoring and error reporting
7. front-end performance
8. tests and coverage

## What you may change without asking
Only changes that are cheap and have no downside: dead code, obvious bugs with a test, missing indexes with no write cost, N+1 queries, error messages, caching that cannot serve stale data, small refactors covered by tests. Work on a branch, run the tests, commit each change on its own with a clear message, then merge and deploy the way this project is deployed (fill in: ...). Log each change with clawhq_log kind=change.

## What needs an issue
Schema changes, migrations, dependency upgrades, config or secrets, deletes, anything touching money or customer data, anything you are not sure about. Open the issue with your recommendation and stop there.

## Reporting
At the end of each run, one clawhq_log line: what was checked, how many changes, how many issues opened.
`

// Wire writes the project's MCP config, hooks, CLAUDE.md section and a mission.
func (s *HelperService) Wire(ctx context.Context, id, projectID string) (HelperStatus, error) {
	p, err := s.profile(id)
	if err != nil {
		return HelperStatus{}, err
	}
	var pr store.ServerProject
	found := false
	for _, x := range p.Projects {
		if x.ID == projectID {
			pr, found = x, true
		}
	}
	if !found {
		return HelperStatus{}, fmt.Errorf("no project %q", projectID)
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return HelperStatus{}, err
	}
	home, _ := c.sftp.Getwd()
	dir := pr.Dir
	if dir == "" {
		dir = home
	}
	helperPath := path.Join(home, helperRemote)
	if _, err := c.sftp.Stat(helperPath); err != nil {
		if _, err := s.Install(ctx, id); err != nil {
			return HelperStatus{}, err
		}
	}

	// .mcp.json: merge the clawhq server in.
	mcp := map[string]any{}
	if raw, err := readRemote(c.sftp, path.Join(dir, ".mcp.json")); err == nil {
		_ = json.Unmarshal([]byte(raw), &mcp)
	}
	servers, _ := mcp["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["clawhq"] = map[string]any{"command": "node", "args": []string{helperPath, "mcp", "--project", dir}}
	mcp["mcpServers"] = servers
	b, _ := json.MarshalIndent(mcp, "", "  ")
	if err := writeRemote(c.sftp, path.Join(dir, ".mcp.json"), string(b)+"\n", 0o644); err != nil {
		return HelperStatus{}, err
	}

	// .claude/settings.json: merge the hooks in.
	settings := map[string]any{}
	if raw, err := readRemote(c.sftp, path.Join(dir, ".claude", "settings.json")); err == nil {
		_ = json.Unmarshal([]byte(raw), &settings)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for _, ev := range []string{"Stop", "SessionEnd"} {
		cmd := "node " + shq(helperPath) + " hook " + ev
		list, _ := hooks[ev].([]any)
		have := false
		for _, entry := range list {
			if raw, _ := json.Marshal(entry); strings.Contains(string(raw), helperPath) {
				have = true
			}
		}
		if !have {
			list = append(list, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": 20}}})
		}
		hooks[ev] = list
	}
	settings["hooks"] = hooks
	// Let the project's MCP server load without a prompt.
	settings["enableAllProjectMcpServers"] = true
	b, _ = json.MarshalIndent(settings, "", "  ")
	if err := writeRemote(c.sftp, path.Join(dir, ".claude", "settings.json"), string(b)+"\n", 0o644); err != nil {
		return HelperStatus{}, err
	}

	// CLAUDE.md: append the section once.
	md, _ := readRemote(c.sftp, path.Join(dir, "CLAUDE.md"))
	if !strings.Contains(md, "## ClawHQ") {
		if err := writeRemote(c.sftp, path.Join(dir, "CLAUDE.md"), strings.TrimRight(md, "\n")+claudeMdSection, 0o644); err != nil {
			return HelperStatus{}, err
		}
	}
	// Mission: only when missing.
	if _, err := c.sftp.Stat(path.Join(dir, ".clawhq", "MISSION.md")); err != nil {
		if err := writeRemote(c.sftp, path.Join(dir, ".clawhq", "MISSION.md"), missionTemplate, 0o644); err != nil {
			return HelperStatus{}, err
		}
	}
	_ = c.sftp.MkdirAll(path.Join(dir, ".clawhq", "issues"))
	log.Printf("helper: wired %s on %s", dir, id)
	return s.Status(ctx, id)
}

// Unwire removes the hooks and MCP entry; files under .clawhq stay.
func (s *HelperService) Unwire(ctx context.Context, id, projectID string) (HelperStatus, error) {
	p, err := s.profile(id)
	if err != nil {
		return HelperStatus{}, err
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return HelperStatus{}, err
	}
	home, _ := c.sftp.Getwd()
	helperPath := path.Join(home, helperRemote)
	for _, pr := range p.Projects {
		if pr.ID != projectID {
			continue
		}
		dir := pr.Dir
		if dir == "" {
			dir = home
		}
		mcp := map[string]any{}
		if raw, err := readRemote(c.sftp, path.Join(dir, ".mcp.json")); err == nil {
			_ = json.Unmarshal([]byte(raw), &mcp)
			if servers, ok := mcp["mcpServers"].(map[string]any); ok {
				delete(servers, "clawhq")
			}
			b, _ := json.MarshalIndent(mcp, "", "  ")
			_ = writeRemote(c.sftp, path.Join(dir, ".mcp.json"), string(b)+"\n", 0o644)
		}
		settings := map[string]any{}
		if raw, err := readRemote(c.sftp, path.Join(dir, ".claude", "settings.json")); err == nil {
			_ = json.Unmarshal([]byte(raw), &settings)
			if hooks, ok := settings["hooks"].(map[string]any); ok {
				for ev, v := range hooks {
					list, _ := v.([]any)
					kept := []any{}
					for _, entry := range list {
						if raw, _ := json.Marshal(entry); !strings.Contains(string(raw), helperPath) {
							kept = append(kept, entry)
						}
					}
					hooks[ev] = kept
				}
			}
			b, _ := json.MarshalIndent(settings, "", "  ")
			_ = writeRemote(c.sftp, path.Join(dir, ".claude", "settings.json"), string(b)+"\n", 0o644)
		}
		if md, err := readRemote(c.sftp, path.Join(dir, "CLAUDE.md")); err == nil && strings.Contains(md, "## ClawHQ") {
			i := strings.Index(md, "\n## ClawHQ")
			if i >= 0 {
				_ = writeRemote(c.sftp, path.Join(dir, "CLAUDE.md"), strings.TrimRight(md[:i], "\n")+"\n", 0o644)
			}
		}
	}
	return s.Status(ctx, id)
}

// Outbox returns events newer than sinceTs, reading the tail of the file.
func (s *HelperService) Outbox(ctx context.Context, id string, sinceTs int64, limit int) ([]OutboxEvent, error) {
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	home, _ := c.sftp.Getwd()
	events, _, err := s.readOutbox(c.sftp, path.Join(home, ".clawhq", "outbox.jsonl"), 0)
	if err != nil {
		return []OutboxEvent{}, nil
	}
	out := []OutboxEvent{}
	for _, e := range events {
		if e.Ts > sinceTs {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// readOutbox reads from an offset (0: the last 1 MB) and returns events plus the new offset.
func (s *HelperService) readOutbox(c *sftp.Client, p string, from int64) ([]OutboxEvent, int64, error) {
	st, err := c.Stat(p)
	if err != nil {
		return nil, 0, err
	}
	f, err := c.Open(p)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	start := from
	if start == 0 && st.Size() > 1<<20 {
		start = st.Size() - 1<<20
	}
	if start > st.Size() {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, 0, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	out := []OutboxEvent{}
	first := start != 0 && from == 0
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false // a partial first line after a seek
			continue
		}
		var e OutboxEvent
		if json.Unmarshal([]byte(line), &e) == nil && e.Ts > 0 {
			out = append(out, e)
		}
	}
	return out, st.Size(), nil
}

// Issues lists a project's numbered issues, open first.
func (s *HelperService) Issues(ctx context.Context, id, projectID string) ([]ServerIssue, error) {
	dir, c, err := s.projectDir(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	raw, err := readRemote(c.sftp, path.Join(dir, ".clawhq", "issues.json"))
	if err != nil {
		return []ServerIssue{}, nil
	}
	var db struct {
		Issues []ServerIssue `json:"issues"`
	}
	if err := json.Unmarshal([]byte(raw), &db); err != nil {
		return nil, err
	}
	rank := map[string]int{"urgent": 0, "high": 1, "normal": 2, "low": 3}
	sort.SliceStable(db.Issues, func(i, j int) bool {
		a, b := db.Issues[i], db.Issues[j]
		if (a.Status == "open") != (b.Status == "open") {
			return a.Status == "open"
		}
		if a.NeedsBoss != b.NeedsBoss {
			return a.NeedsBoss
		}
		if rank[a.Urgency] != rank[b.Urgency] {
			return rank[a.Urgency] < rank[b.Urgency]
		}
		return a.UpdatedAt > b.UpdatedAt
	})
	if db.Issues == nil {
		db.Issues = []ServerIssue{}
	}
	return db.Issues, nil
}

func (s *HelperService) IssueDetails(ctx context.Context, id, projectID string, n int) (string, error) {
	dir, c, err := s.projectDir(ctx, id, projectID)
	if err != nil {
		return "", err
	}
	return readRemote(c.sftp, path.Join(dir, ".clawhq", "issues", fmt.Sprintf("%d.md", n)))
}

func (s *HelperService) Mission(ctx context.Context, id, projectID string) (string, error) {
	dir, c, err := s.projectDir(ctx, id, projectID)
	if err != nil {
		return "", err
	}
	raw, err := readRemote(c.sftp, path.Join(dir, ".clawhq", "MISSION.md"))
	if err != nil {
		return "", nil
	}
	return raw, nil
}

func (s *HelperService) SetMission(ctx context.Context, id, projectID, text string) error {
	dir, c, err := s.projectDir(ctx, id, projectID)
	if err != nil {
		return err
	}
	return writeRemote(c.sftp, path.Join(dir, ".clawhq", "MISSION.md"), text, 0o644)
}

func (s *HelperService) projectDir(ctx context.Context, id, projectID string) (string, *sftpConn, error) {
	p, err := s.profile(id)
	if err != nil {
		return "", nil, err
	}
	c, err := s.files.conn(ctx, id)
	if err != nil {
		return "", nil, err
	}
	home, _ := c.sftp.Getwd()
	pr := p.Active()
	for _, x := range p.Projects {
		if x.ID == projectID {
			pr = x
		}
	}
	dir := pr.Dir
	if dir == "" {
		dir = home
	}
	return dir, c, nil
}

// ---- the live session: an interactive Claude Code in tmux -------------------------

// SendToSession types a message into a tmux session (bracketed paste, then Enter),
// so the interactive Claude Code there gets it as one message.
func (s *HelperService) SendToSession(ctx context.Context, id, session, text string) error {
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
	cmd := "tmux set-buffer -b clawhq -- " + shq(text) + " && tmux paste-buffer -p -b clawhq -t " + shq(name) + " && sleep 0.3 && tmux send-keys -t " + shq(name) + " Enter"
	res, err := sshx.Run(ctx, client, cmd, 20*time.Second)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("tmux: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return nil
}

// StartSession opens (or reuses) a tmux session running interactive Claude Code in
// the project, optionally resuming a Claude session id. Returns the tmux name.
func (s *HelperService) StartSession(ctx context.Context, id, projectID, resume string) (string, error) {
	p, err := s.profile(id)
	if err != nil {
		return "", err
	}
	pr := p.Active()
	for _, x := range p.Projects {
		if x.ID == projectID {
			pr = x
		}
	}
	name := "clawhq-live-" + tmuxNameRe.ReplaceAllString(strings.ToLower(pr.Name), "-")
	client, err := sshx.Dial(ctx, target(p), knownHostsPath())
	if err != nil {
		return "", err
	}
	defer client.Close()
	dir := pr.Dir
	claude := "claude"
	if strings.TrimSpace(resume) != "" {
		claude += " --resume " + shq(strings.TrimSpace(resume))
	}
	inner := `export PATH="$HOME/.local/bin:$PATH"; ` + claude
	cd := ""
	if dir != "" {
		cd = "-c " + shq(dir) + " "
	}
	cmd := "tmux has-session -t " + shq(name) + " 2>/dev/null || tmux new-session -d -s " + shq(name) + " " + cd + shq("bash -lc "+shq(inner)) + " \\; set -g mouse on \\; set -g history-limit 20000 \\; set -g status off \\; set -s set-clipboard on"
	res, err := sshx.Run(ctx, client, cmd, 20*time.Second)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("tmux: %s", strings.TrimSpace(res.Stderr+res.Stdout))
	}
	return name, nil
}

// ---- watcher: new outbox events ring the bell -----------------------------------

func (s *HelperService) watch() {
	time.Sleep(2 * time.Minute)
	for {
		for _, p := range s.store.Read().Servers {
			s.mu.Lock()
			present := s.present[p.ID]
			s.mu.Unlock()
			if !present {
				continue
			}
			s.poll(p)
		}
		time.Sleep(60 * time.Second)
	}
}

func (s *HelperService) poll(p store.ServerProfile) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c, err := s.files.conn(ctx, p.ID)
	if err != nil {
		return
	}
	home, _ := c.sftp.Getwd()
	s.mu.Lock()
	if s.offsets == nil {
		s.offsets = map[string]int64{}
	}
	from, seen := s.offsets[p.ID]
	s.mu.Unlock()
	file := path.Join(home, ".clawhq", "outbox.jsonl")
	if !seen {
		// First look: remember the end, do not replay history as notifications.
		if st, err := c.sftp.Stat(file); err == nil {
			s.mu.Lock()
			s.offsets[p.ID] = st.Size()
			s.mu.Unlock()
		}
		return
	}
	events, size, err := s.readOutbox(c.sftp, file, from)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.offsets[p.ID] = size
	s.mu.Unlock()
	for _, e := range events {
		if s.app != nil {
			s.app.Event.Emit("helper:event", map[string]any{"serverId": p.ID, "event": e})
		}
		if s.notify == nil {
			continue
		}
		proj := path.Base(e.Project)
		switch {
		case e.Type == "issue" && e.NeedsBoss:
			s.notify.show(store.Notice{Title: fmt.Sprintf("#%d needs you · %s / %s", e.N, p.Name, proj), Body: e.Title, AgentName: "Claude Code", AgentEmoji: "🧑‍💻", Origin: "server:" + p.ID, AtMs: e.Ts})
		case e.Type == "log" && e.Kind == "warn":
			s.notify.show(store.Notice{Title: "Warning · " + p.Name + " / " + proj, Body: e.Text, AgentName: "Claude Code", AgentEmoji: "🧑‍💻", Origin: "server:" + p.ID, AtMs: e.Ts})
		}
	}
}
