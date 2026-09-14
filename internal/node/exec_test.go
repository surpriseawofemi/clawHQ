package node

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/store"
)

func TestMatchesAllowlist(t *testing.T) {
	entries := []string{"git", "npm run *", "ls -la /tmp"}
	cases := map[string]bool{
		"git status":          true,
		"/usr/bin/git log":    true,
		"npm run build":       true,
		"npm install":         false,
		"ls -la /tmp":         true,
		"ls -la /etc":         false,
		"gitk":                false,
		"rm -rf /":            false,
		"":                    false,
		"  git   diff  HEAD~": true,
	}
	for cmd, want := range cases {
		if got := matchesAllowlist(cmd, entries); got != want {
			t.Errorf("matchesAllowlist(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestSystemRunPrepareEchoesPlan(t *testing.T) {
	h := New(t.TempDir(), nil, nil, nil, nil)
	raw := json.RawMessage(`{"command":["/bin/sh","-lc","echo hi"],"rawCommand":"echo hi","agentId":"main","sessionKey":"agent:main:main"}`)
	payload, failure := h.systemRunPrepare(raw)
	if failure != "" {
		t.Fatalf("unexpected failure: %s", failure)
	}
	plan := payload.(map[string]any)["plan"].(runPlan)
	if plan.CommandText != "echo hi" || len(plan.Argv) != 3 || plan.AgentID != "main" {
		t.Fatalf("plan not echoed: %+v", plan)
	}
	if plan.Cwd == "" {
		t.Fatalf("cwd should default to home")
	}
}

func TestSystemRunPolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	h := New(t.TempDir(), nil, nil, nil, nil)
	run := func(approved bool) (map[string]any, string) {
		raw := json.RawMessage(`{"command":["/bin/sh","-c","echo out; echo err 1>&2; exit 3"],"rawCommand":"echo out","approved":` + boolStr(approved) + `}`)
		payload, failure := h.systemRun(raw)
		res, _ := payload.(runResult)
		out := map[string]any{"stdout": res.Stdout, "stderr": res.Stderr, "success": res.Success}
		if res.ExitCode != nil {
			out["exit"] = *res.ExitCode
		}
		return out, failure
	}

	h.SetExecPolicy(store.ExecOff, nil)
	if _, failure := run(false); !strings.Contains(failure, "switched off") {
		t.Fatalf("off mode should refuse, got %q", failure)
	}

	h.SetExecPolicy(store.ExecAllow, nil)
	res, failure := run(false)
	if failure != "" || res["exit"] != 3 || !strings.Contains(res["stdout"].(string), "out") || !strings.Contains(res["stderr"].(string), "err") {
		t.Fatalf("allow mode should run: res=%v failure=%q", res, failure)
	}

	// Ask mode with nobody listening denies rather than runs.
	h.SetExecPolicy(store.ExecAsk, nil)
	if _, failure := run(false); failure == "" {
		t.Fatalf("ask mode with no approver should fail")
	}
	// The gateway already asked: honoured without a second prompt.
	if _, failure := run(true); failure != "" {
		t.Fatalf("approved command should run, got %q", failure)
	}
	// Allowlisted runs silently.
	h.SetExecPolicy(store.ExecAsk, []string{"echo out"})
	if _, failure := run(false); failure != "" {
		t.Fatalf("allowlisted command should run, got %q", failure)
	}

	// Ask mode with an approver: the decision flows back through ResolveExec.
	h.SetExecPolicy(store.ExecAsk, nil)
	asked := make(chan ExecRequest, 1)
	h.SetHooks(Hooks{OnExecRequest: func(r ExecRequest) { asked <- r }})
	done := make(chan string, 1)
	go func() {
		_, failure := run(false)
		done <- failure
	}()
	select {
	case req := <-asked:
		if len(h.PendingExec()) != 1 {
			t.Fatalf("pending list should hold the request")
		}
		if err := h.ResolveExec(req.ID, ExecDecisionDeny); err != nil {
			t.Fatalf("resolve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no exec request raised")
	}
	if failure := <-done; !strings.Contains(failure, "declined") {
		t.Fatalf("deny should be reported, got %q", failure)
	}
	if len(h.PendingExec()) != 0 {
		t.Fatalf("pending list should be empty after resolve")
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestRunCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	res := runCommand([]string{"/bin/sh", "-c", "sleep 5"}, t.TempDir(), nil, 200*time.Millisecond)
	if !res.TimedOut || res.Success {
		t.Fatalf("expected timeout, got %+v", res)
	}
}
