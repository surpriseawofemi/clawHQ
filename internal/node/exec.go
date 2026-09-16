package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/store"
)

// Shell commands, OpenClaw's own names. The gateway's exec tool with host=node calls
// system.run.prepare to get a canonical plan, runs its own approval policy, then calls
// system.run with that plan. The generic node.invoke RPC refuses both, so an agent
// cannot reach them except through the exec tool.
const (
	CmdSystemRunPrepare  = "system.run.prepare"
	CmdSystemRun         = "system.run"
	CmdExecApprovalsGet  = "system.execApprovals.get"
	CmdExecApprovalsSet  = "system.execApprovals.set"
	execApprovalsVersion = 1
)

// Exec decisions the user can give on a pending command.
const (
	ExecDecisionAllow  = "allow"
	ExecDecisionAlways = "always"
	// ExecDecisionTrust runs this command and sets the agent to run without asking.
	ExecDecisionTrust = "trust"
	ExecDecisionDeny  = "deny"
)

// ExecRequest is a command waiting for the user's decision. It is pushed to the
// frontend, which shows it as a banner with allow / always / deny.
type ExecRequest struct {
	ID          string   `json:"id"`
	Command     string   `json:"command"`
	Argv        []string `json:"argv"`
	Cwd         string   `json:"cwd"`
	AgentID     string   `json:"agentId"`
	SessionKey  string   `json:"sessionKey"`
	AtMs        int64    `json:"atMs"`
	ExpiresAtMs int64    `json:"expiresAtMs"`
}

// ExecRecord is what the audit log keeps about each system.run: the store's shape,
// filled in here once the decision and the result are known.
type ExecRecord = store.ExecRecord

// Decisions as they are written to the audit log.
const (
	recAllowlisted = "allowlisted"
	recAllowed     = "allowed"
	recAlways      = "always"
	recTrusted     = "trusted-agent"
	recGateway     = "approved-by-gateway"
	recNoAsk       = "run-without-asking"
	recDenied      = "denied"
	recTimedOut    = "timed-out"
	recOff         = "off"
	recRefused     = "refused"
)

// execGate holds the pending-approval state for system.run.
type execGate struct {
	mu      sync.Mutex
	mode    string
	allow   []string
	agents  map[string]string
	pending map[string]chan string
	seq     int
}

// Output caps keep a runaway command from flooding the gateway; the exec tool shows
// the tail anyway.
const (
	execMaxOutput      = 256 * 1024
	execDefaultTimeout = 60 * time.Second
	execMaxTimeout     = 10 * time.Minute
	execAskTimeout     = 2 * time.Minute
)

// SetExecPolicy replaces the exec mode, the allowlist and the per-agent overrides.
func (h *Host) SetExecPolicy(mode string, allow []string, agents map[string]string) {
	agentModes := map[string]string{}
	for id, m := range agents {
		switch m {
		case store.ExecOff, store.ExecAsk, store.ExecAllow:
			agentModes[id] = m
		}
	}
	h.exec.mu.Lock()
	switch mode {
	case store.ExecOff, store.ExecAsk, store.ExecAllow:
		h.exec.mode = mode
	default:
		h.exec.mode = store.ExecAsk
	}
	h.exec.allow = append([]string(nil), allow...)
	h.exec.agents = agentModes
	h.exec.mu.Unlock()
	h.setStatus(func(s *Status) {
		s.ExecMode = mode
		s.ExecAllow = append([]string(nil), allow...)
		s.ExecAgents = agentModes
	})
}

// modeFor is the mode that applies to one agent: its override when it has one,
// the machine-wide mode otherwise.
func (h *Host) modeFor(agentID string) (mode string, overridden bool) {
	h.exec.mu.Lock()
	defer h.exec.mu.Unlock()
	if m, ok := h.exec.agents[agentID]; ok && agentID != "" {
		return m, true
	}
	return h.exec.mode, false
}

// PendingExec lists commands still waiting for a decision, oldest first, so a
// banner dismissed by mistake can be brought back.
func (h *Host) PendingExec() []ExecRequest {
	h.exec.mu.Lock()
	defer h.exec.mu.Unlock()
	out := make([]ExecRequest, 0, len(h.pendingExec))
	for _, req := range h.pendingExec {
		out = append(out, req)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AtMs < out[j].AtMs })
	return out
}

// ResolveExec answers a pending request. "always" also adds the command to the
// allowlist through the onAllowAlways hook, so the next identical call runs silently.
func (h *Host) ResolveExec(id, decision string) error {
	h.exec.mu.Lock()
	ch, ok := h.exec.pending[id]
	req := h.pendingExec[id]
	h.exec.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending command with id %q", id)
	}
	switch decision {
	case ExecDecisionAllow, ExecDecisionAlways, ExecDecisionDeny:
	case ExecDecisionTrust:
		if req.AgentID == "" {
			return errors.New("this request does not say which agent sent it, so it cannot be trusted by agent")
		}
	default:
		return fmt.Errorf("unknown decision %q", decision)
	}
	if decision == ExecDecisionAlways && h.onAllowAlways != nil {
		h.onAllowAlways(req.Command)
	}
	if decision == ExecDecisionTrust && h.onTrustAgent != nil {
		h.onTrustAgent(req.AgentID)
	}
	select {
	case ch <- decision:
	default:
		// Already answered or timed out; nothing to do.
	}
	return nil
}

// allowlisted reports whether the command text matches an allowlist entry.
func (h *Host) allowlisted(commandText string) bool {
	h.exec.mu.Lock()
	entries := append([]string(nil), h.exec.allow...)
	h.exec.mu.Unlock()
	return matchesAllowlist(commandText, entries)
}

// matchesAllowlist implements the three entry forms documented on ExecConfig.
func matchesAllowlist(commandText string, entries []string) bool {
	text := strings.TrimSpace(commandText)
	if text == "" {
		return false
	}
	first := strings.Fields(text)
	firstWord := ""
	if len(first) > 0 {
		firstWord = filepath.Base(first[0])
	}
	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		switch {
		case entry == "":
			continue
		case strings.HasSuffix(entry, "*"):
			if strings.HasPrefix(text, strings.TrimSpace(strings.TrimSuffix(entry, "*"))) {
				return true
			}
		case entry == text:
			return true
		case !strings.ContainsAny(entry, " \t") && entry == firstWord:
			return true
		}
	}
	return false
}

// runPlan is the canonical description of a command, echoed back to the gateway by
// system.run.prepare and expected again in system.run.
type runPlan struct {
	Argv        []string `json:"argv"`
	CommandText string   `json:"commandText"`
	Cwd         string   `json:"cwd,omitempty"`
	AgentID     string   `json:"agentId,omitempty"`
	SessionKey  string   `json:"sessionKey,omitempty"`
}

// systemRunPrepare answers system.run.prepare. It does no policy work: the plan is
// what the gateway's approval record binds to, so it must be deterministic.
func (h *Host) systemRunPrepare(raw json.RawMessage) (any, string) {
	var p struct {
		Command    []string `json:"command"`
		RawCommand string   `json:"rawCommand"`
		Cwd        string   `json:"cwd"`
		AgentID    string   `json:"agentId"`
		SessionKey string   `json:"sessionKey"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "bad system.run.prepare params: " + err.Error()
	}
	if len(p.Command) == 0 {
		return nil, "system.run.prepare needs a command"
	}
	text := strings.TrimSpace(p.RawCommand)
	if text == "" {
		text = strings.Join(p.Command, " ")
	}
	return map[string]any{"plan": runPlan{
		Argv:        p.Command,
		CommandText: text,
		Cwd:         resolveCwd(p.Cwd),
		AgentID:     p.AgentID,
		SessionKey:  p.SessionKey,
	}}, ""
}

// resolveCwd turns the gateway's cwd into an absolute directory, defaulting to home.
func resolveCwd(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return home
	}
	if strings.HasPrefix(cwd, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, strings.TrimPrefix(cwd, "~"))
		}
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		return abs
	}
	return cwd
}

// systemRun answers system.run: decide, then run.
//
// The gateway sends approved=true when its own policy prompted an operator and they
// said yes; that is honoured so the user is not asked twice. Otherwise ClawHQ's mode
// applies: off refuses, allow runs, ask consults the allowlist and then the user.
func (h *Host) systemRun(raw json.RawMessage) (any, string) {
	var p struct {
		Command    []string          `json:"command"`
		RawCommand string            `json:"rawCommand"`
		Plan       *runPlan          `json:"systemRunPlan"`
		Cwd        string            `json:"cwd"`
		Env        map[string]string `json:"env"`
		TimeoutMs  int               `json:"timeoutMs"`
		AgentID    string            `json:"agentId"`
		SessionKey string            `json:"sessionKey"`
		Approved   bool              `json:"approved"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "bad system.run params: " + err.Error()
	}

	argv := p.Command
	text := strings.TrimSpace(p.RawCommand)
	cwd := p.Cwd
	if p.Plan != nil {
		if len(p.Plan.Argv) > 0 {
			argv = p.Plan.Argv
		}
		if p.Plan.CommandText != "" {
			text = p.Plan.CommandText
		}
		if cwd == "" {
			cwd = p.Plan.Cwd
		}
		if p.AgentID == "" {
			p.AgentID = p.Plan.AgentID
		}
		if p.SessionKey == "" {
			p.SessionKey = p.Plan.SessionKey
		}
	}
	if len(argv) == 0 {
		return nil, "system.run needs a command"
	}
	if text == "" {
		text = strings.Join(argv, " ")
	}
	cwd = resolveCwd(cwd)

	mode, perAgent := h.modeFor(p.AgentID)

	rec := ExecRecord{
		AtMs: time.Now().UnixMilli(), AgentID: p.AgentID, SessionKey: p.SessionKey,
		Command: text, Cwd: cwd,
	}
	refuse := func(decision, msg string) (any, string) {
		rec.Decision, rec.Error = decision, msg
		h.record(rec)
		return nil, msg
	}

	switch {
	case mode == store.ExecOff:
		return refuse(recOff, "agent commands are switched off on this machine — turn them on in ClawHQ → Settings → node")
	case p.Approved:
		rec.Decision = recGateway
	case mode == store.ExecAllow && perAgent:
		rec.Decision = recTrusted
	case mode == store.ExecAllow:
		rec.Decision = recNoAsk
	case h.allowlisted(text):
		rec.Decision = recAllowlisted
	case mode == store.ExecAsk:
		decision, err := h.askUser(text, argv, cwd, p.AgentID, p.SessionKey)
		if err != nil {
			return refuse(recTimedOut, err.Error())
		}
		switch decision {
		case ExecDecisionDeny:
			return refuse(recDenied, "the user declined to run this command")
		case ExecDecisionAlways:
			rec.Decision = recAlways
		case ExecDecisionTrust:
			rec.Decision = recTrusted
		default:
			rec.Decision = recAllowed
		}
	default:
		return refuse(recRefused, "agent commands are not allowed on this machine")
	}

	timeout := execDefaultTimeout
	if p.TimeoutMs > 0 {
		timeout = time.Duration(p.TimeoutMs) * time.Millisecond
	}
	if timeout > execMaxTimeout {
		timeout = execMaxTimeout
	}
	res := runCommand(argv, cwd, p.Env, timeout)
	rec.Ran, rec.ExitCode, rec.Success, rec.TimedOut = true, res.ExitCode, res.Success, res.TimedOut
	rec.DurationMs, rec.Error = res.DurationMs, res.Error
	rec.Output = res.Stdout
	if res.Stderr != "" {
		rec.Output = strings.TrimRight(rec.Output, "\n") + "\n" + res.Stderr
	}
	h.record(rec)
	return res, ""
}

// record hands a finished entry to the audit log, when one is attached.
func (h *Host) record(rec ExecRecord) {
	if h.onExecRecord != nil {
		h.onExecRecord(rec)
	}
}

// askUser raises a request to the frontend and waits for the answer. A request that
// nobody answers is denied, never silently run.
func (h *Host) askUser(text string, argv []string, cwd, agentID, sessionKey string) (string, error) {
	if h.onExecRequest == nil {
		return "", errors.New("no one is available to approve this command")
	}
	ch := make(chan string, 1)
	now := time.Now()
	h.exec.mu.Lock()
	h.exec.seq++
	id := fmt.Sprintf("exec-%d-%d", now.UnixNano(), h.exec.seq)
	req := ExecRequest{
		ID: id, Command: text, Argv: argv, Cwd: cwd,
		AgentID: agentID, SessionKey: sessionKey,
		AtMs: now.UnixMilli(), ExpiresAtMs: now.Add(execAskTimeout).UnixMilli(),
	}
	if h.exec.pending == nil {
		h.exec.pending = map[string]chan string{}
	}
	if h.pendingExec == nil {
		h.pendingExec = map[string]ExecRequest{}
	}
	h.exec.pending[id] = ch
	h.pendingExec[id] = req
	h.exec.mu.Unlock()

	defer func() {
		h.exec.mu.Lock()
		delete(h.exec.pending, id)
		delete(h.pendingExec, id)
		h.exec.mu.Unlock()
		h.emitExecPending()
	}()

	h.emitExecPending()
	h.onExecRequest(req)

	select {
	case decision := <-ch:
		return decision, nil
	case <-time.After(execAskTimeout):
		return "", errors.New("nobody approved this command in time")
	}
}

// emitExecPending mirrors the pending list into the status so the UI can restore
// banners after a reload.
func (h *Host) emitExecPending() {
	pending := h.PendingExec()
	h.setStatus(func(s *Status) { s.PendingExec = pending })
}

// runResult mirrors what the gateway's exec tool reads back from a node.
type runResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   *int   `json:"exitCode"`
	Success    bool   `json:"success"`
	TimedOut   bool   `json:"timedOut"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

// cappedBuffer keeps the first execMaxOutput bytes and notes that more followed.
type cappedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	room := execMaxOutput - c.buf.Len()
	if room <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		c.truncated = true
		p = p[:room]
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	if c.truncated {
		return c.buf.String() + "\n[output truncated by ClawHQ]"
	}
	return c.buf.String()
}

func runCommand(argv []string, cwd string, env map[string]string, timeout time.Duration) runResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	started := time.Now()
	err := cmd.Run()
	res := runResult{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMs: time.Since(started).Milliseconds(),
		TimedOut:   errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		code := 0
		res.ExitCode = &code
		res.Success = true
	case errors.As(err, &exitErr):
		code := exitErr.ExitCode()
		res.ExitCode = &code
		if res.TimedOut {
			res.Error = fmt.Sprintf("timed out after %s", timeout)
		}
	default:
		res.Error = err.Error()
	}
	return res
}

// execApprovalsGet reports ClawHQ's policy in the shape the gateway's exec tool
// reads from a node host. It only matters when the gateway's own exec policy is set
// to consult nodes; the mapping is deliberately conservative.
func (h *Host) execApprovalsGet() any {
	h.exec.mu.Lock()
	mode := h.exec.mode
	h.exec.mu.Unlock()
	security, ask := "deny", "off"
	switch mode {
	case store.ExecAllow:
		security = "full"
	case store.ExecAsk:
		security, ask = "allowlist", "on-miss"
	}
	file := map[string]any{
		"version": execApprovalsVersion,
		"defaults": map[string]any{
			"security":    security,
			"ask":         ask,
			"askFallback": "deny",
		},
		"agents": map[string]any{},
	}
	// The gateway validates this against its node-snapshot schema, which requires
	// path, exists, hash and file together; leaving any out is "invalid payload".
	return map[string]any{"path": "clawhq://exec-policy", "exists": true, "file": file, "hash": "clawhq-" + mode}
}

// execApprovalsSet accepts a policy pushed from the gateway's Control UI and maps its
// security level onto ClawHQ's mode. Allowlist entries there are binary path patterns
// rather than command text, so they are not imported.
func (h *Host) execApprovalsSet(raw json.RawMessage) (any, string) {
	var p struct {
		File struct {
			Defaults struct {
				Security string `json:"security"`
			} `json:"defaults"`
		} `json:"file"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "bad system.execApprovals.set params: " + err.Error()
	}
	mode := ""
	switch p.File.Defaults.Security {
	case "deny":
		mode = store.ExecOff
	case "allowlist":
		mode = store.ExecAsk
	case "full":
		mode = store.ExecAllow
	default:
		return nil, fmt.Sprintf("unknown security level %q", p.File.Defaults.Security)
	}
	if h.onExecModeChanged != nil {
		h.onExecModeChanged(mode)
	}
	return map[string]any{"ok": true, "hash": mode}, ""
}
