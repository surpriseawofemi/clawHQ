// Package node lets ClawHQ act as an OpenClaw *node* in addition to an operator.
//
// The two roles point in opposite directions. As an operator ClawHQ reads gateway
// state and sends chat; as a node it exposes this machine's capabilities so agents
// running on the gateway can reach them. That is what makes "read the code in this
// folder on my Mac" work when the gateway lives somewhere else entirely.
//
// The node connects outbound over the same WebSocket protocol, so it needs no inbound
// ports and works identically over an SSH tunnel, a Cloudflare tunnel, or a tailnet.
package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/store"

	ocgateway "github.com/a3tai/openclaw-go/gateway"
	"github.com/a3tai/openclaw-go/identity"
	"github.com/a3tai/openclaw-go/protocol"
)

// Commands ClawHQ advertises. These names are not ours to invent — they mirror what
// a real `openclaw node` host exposes, so the gateway's existing tooling works against
// ClawHQ unchanged.
const (
	CmdSystemWhich = "system.which"
	CmdFsListDir   = "fs.listDir"

	// ClawHQ's own verbs. These are not part of OpenClaw's node surface — they let
	// agents manage the org chart that ClawHQ owns, which lives in clawhq.json and
	// has no gateway representation at all.
	CmdDepartmentsList  = "clawhq.departments.list"
	CmdDepartmentCreate = "clawhq.departments.create"
	CmdAgentAssign      = "clawhq.agents.assign"
)

// commands is the advertised surface. Changing this list raises a fresh pairing
// approval on the gateway, by design.
var commands = []string{
	CmdSystemWhich,
	CmdFsListDir,
	CmdScreenSnapshot,
	CmdDepartmentsList,
	CmdDepartmentCreate,
	CmdAgentAssign,
}

// Caps ClawHQ claims. "file" covers directory browsing, "system" covers binary lookup.
var Caps = []string{"file", "system", "screen"}

// DepartmentStore is the slice of ClawHQ's config the node is allowed to touch.
// Narrow on purpose: agents can shape the org chart and nothing else.
type DepartmentStore interface {
	Read() store.Config
	UpsertDepartment(dept store.Department) (store.Config, error)
	AssignAgent(agentID, departmentID string) (store.Config, error)
}

type Status struct {
	Enabled       bool     `json:"enabled"`
	Connected     bool     `json:"connected"`
	GatewayID     string   `json:"gatewayId"`
	DeviceID      string   `json:"deviceId"`
	SharedFolders []string `json:"sharedFolders"`
	Commands      []string `json:"commands"`
	Error         string   `json:"error"`
	// LastInvoke is a short human-readable trace of the most recent request, which is
	// the difference between "it silently does nothing" and a debuggable feature.
	LastInvoke string `json:"lastInvoke"`
}

// Host is ClawHQ's node-role connection.
type Host struct {
	mu     sync.RWMutex
	client *ocgateway.Client
	status Status

	identityRoot  string
	sharedFolders []string
	store         DepartmentStore

	emitStatus func(Status)
	// logUnknown records invokes we do not implement yet, so the command surface can
	// be extended against real traffic instead of guesswork.
	logUnknown func(command string, params string)
}

func New(identityRoot string, st DepartmentStore, emitStatus func(Status), logUnknown func(string, string)) *Host {
	return &Host{
		identityRoot: identityRoot,
		store:        st,
		emitStatus:   emitStatus,
		logUnknown:   logUnknown,
		status:       Status{Commands: commands},
	}
}

func (h *Host) Status() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

func (h *Host) setStatus(mutate func(*Status)) {
	h.mu.Lock()
	mutate(&h.status)
	snapshot := h.status
	h.mu.Unlock()
	if h.emitStatus != nil {
		h.emitStatus(snapshot)
	}
}

// SetSharedFolders replaces the set of directories the node will expose. Everything
// outside them is refused, so this list is the entire blast radius of the node role.
func (h *Host) SetSharedFolders(folders []string) {
	cleaned := make([]string, 0, len(folders))
	for _, f := range folders {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if abs, err := filepath.Abs(f); err == nil {
			f = abs
		}
		cleaned = append(cleaned, filepath.Clean(f))
	}
	h.mu.Lock()
	h.sharedFolders = cleaned
	h.mu.Unlock()
	h.setStatus(func(s *Status) { s.SharedFolders = cleaned })
}

// identityStore keeps the node role's device identity separate from the operator's.
//
// The upstream identity store holds a single device token, and a device needs one token
// per role, so the node gets its own keypair. ClawHQ therefore shows up twice in
// `openclaw devices list` — once as an operator, once as a node.
func (h *Host) identityStore(gatewayID string) (*identity.Store, error) {
	if gatewayID == "" {
		gatewayID = "default"
	}
	return identity.NewStore(filepath.Join(h.identityRoot, gatewayID+"-node"))
}

// Start connects the node role. Pass the gateway's shared token on first pairing;
// afterwards the stored device token is enough.
func (h *Host) Start(ctx context.Context, gatewayID, url, token string) (Status, error) {
	h.Stop()

	store, err := h.identityStore(gatewayID)
	if err != nil {
		return h.Status(), fmt.Errorf("node identity store: %w", err)
	}
	id, err := store.LoadOrGenerate()
	if err != nil {
		return h.Status(), fmt.Errorf("node identity: %w", err)
	}

	h.setStatus(func(s *Status) {
		s.Enabled = true
		s.GatewayID = gatewayID
		s.DeviceID = id.DeviceID
		s.Error = ""
	})

	opts := []ocgateway.Option{
		ocgateway.WithClientInfo(protocol.ClientInfo{
			ID:       protocol.ClientIDNodeHost,
			Version:  "0.2.0",
			Mode:     "node",
			Platform: platform(),
		}),
		ocgateway.WithRole(protocol.RoleNode),
		// The client defaults to operator scopes, which a node may not request — the
		// gateway refuses the pairing with "invalid scope for requested roles".
		ocgateway.WithScopes(),
		ocgateway.WithCaps(Caps...),
		ocgateway.WithCommands(commands...),
		ocgateway.WithIdentity(id, ""),
		// A 2026.9.4 gateway does not send `invoke` frames; it emits
		// `node.invoke.request` events and expects the answer as a `node.invoke.result`
		// RPC. WithOnInvoke is kept for older gateways that still use frames.
		ocgateway.WithOnInvoke(h.handleInvoke),
		ocgateway.WithOnEvent(h.handleEvent),
	}
	if strings.TrimSpace(token) != "" {
		opts = append(opts, ocgateway.WithToken(token))
	} else {
		deviceToken := strings.TrimSpace(store.LoadDeviceToken())
		if deviceToken == "" {
			err := fmt.Errorf("node role is not paired yet — supply the gateway token once")
			h.setStatus(func(s *Status) { s.Error = err.Error() })
			return h.Status(), err
		}
		opts = append(opts, ocgateway.WithDeviceToken(deviceToken))
	}

	client := ocgateway.NewClient(opts...)
	if err := client.Connect(ctx, url); err != nil {
		h.setStatus(func(s *Status) { s.Connected = false; s.Error = err.Error() })
		if isAwaitingApproval(err) {
			// Node pairing always needs an operator to approve it, so retry quietly
			// until they do rather than making the user come back and toggle this.
			h.waitForApproval(gatewayID, url, token)
		}
		return h.Status(), err
	}

	if hello := client.Hello(); hello != nil && hello.Auth != nil && hello.Auth.DeviceToken != "" {
		_ = store.SaveDeviceToken(hello.Auth.DeviceToken)
	}

	h.mu.Lock()
	h.client = client
	h.mu.Unlock()

	h.setStatus(func(s *Status) { s.Connected = true; s.Error = "" })

	// Custom verbs reach agents as published tool descriptors, not as advertised
	// commands, so announce them once the session is live.
	h.publishPluginTools(ctx)

	return h.Status(), nil
}

// Commands reports the surface this node advertises.
func Commands() []string { return append([]string(nil), commands...) }

// isAwaitingApproval recognises the gateway's "not approved yet" refusal. The wording
// seen from a 2026.9.4 gateway is:
//
//	NOT_PAIRED: pairing required: device is not approved yet
func isAwaitingApproval(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{"not_paired", "not approved", "pairing required", "pending"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// waitForApproval retries the node connection until an operator approves this device.
func (h *Host) waitForApproval(gatewayID, url, token string) {
	go func() {
		deadline := time.Now().Add(10 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(5 * time.Second)
			status := h.Status()
			if !status.Enabled || status.Connected {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			next, _ := h.Start(ctx, gatewayID, url, token)
			cancel()
			if next.Connected {
				return
			}
		}
	}()
}

func (h *Host) Stop() {
	h.mu.Lock()
	client := h.client
	h.client = nil
	h.mu.Unlock()

	if client != nil {
		_ = client.Close()
	}
	h.setStatus(func(s *Status) { s.Connected = false })
}

// Disable stops the node and marks it off, so it does not restart on next launch.
func (h *Host) Disable() {
	h.Stop()
	h.setStatus(func(s *Status) { s.Enabled = false })
}

// invokeRequest is the payload of a `node.invoke.request` event. Note that params
// arrive as a JSON *string*, not an object.
type invokeRequest struct {
	ID         string `json:"id"`
	NodeID     string `json:"nodeId"`
	Command    string `json:"command"`
	ParamsJSON string `json:"paramsJSON"`
	TimeoutMs  int    `json:"timeoutMs"`
}

// handleEvent routes gateway events, turning invoke requests into command runs.
func (h *Host) handleEvent(ev protocol.Event) {
	if ev.EventName != protocol.EventNodeInvokeRequest {
		return
	}
	var req invokeRequest
	if err := json.Unmarshal(ev.Payload, &req); err != nil {
		return
	}
	go h.runInvoke(req)
}

// runInvoke executes one command and reports the outcome back to the gateway.
func (h *Host) runInvoke(req invokeRequest) {
	h.setStatus(func(s *Status) {
		s.LastInvoke = fmt.Sprintf("%s at %s", req.Command, time.Now().Format("15:04:05"))
	})

	params := json.RawMessage(req.ParamsJSON)
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}

	var payload any
	var failure string

	switch req.Command {
	case CmdSystemWhich:
		payload = h.systemWhich(params)
	case CmdFsListDir:
		payload = h.fsListDir(params)
	case CmdScreenSnapshot:
		payload, failure = h.screenSnapshot(params)
	case CmdDepartmentsList:
		payload = h.departmentsList()
	case CmdDepartmentCreate:
		payload, failure = h.departmentCreate(params)
	case CmdAgentAssign:
		payload, failure = h.agentAssign(params)
	default:
		failure = fmt.Sprintf("ClawHQ does not implement %q", req.Command)
		if h.logUnknown != nil {
			h.logUnknown(req.Command, req.ParamsJSON)
		}
	}

	h.mu.RLock()
	client := h.client
	h.mu.RUnlock()
	if client == nil {
		return
	}

	result := map[string]any{"id": req.ID, "nodeId": req.NodeID}
	if failure != "" {
		result["ok"] = false
		result["error"] = map[string]any{"code": "CLAWHQ_NODE_ERROR", "message": failure}
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			result["ok"] = false
			result["error"] = map[string]any{"code": "CLAWHQ_NODE_ERROR", "message": err.Error()}
		} else {
			result["ok"] = true
			result["payloadJSON"] = string(encoded)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := client.Send(ctx, "node.invoke.result", result)
	if err != nil {
		h.setStatus(func(s *Status) { s.Error = "invoke result failed: " + err.Error() })
		return
	}
	if !resp.OK && resp.Error != nil {
		// Surfaced rather than swallowed: a rejected result means the reply shape is
		// wrong, and the invoke will just time out on the caller with no clue why.
		msg := resp.Error.Message
		h.setStatus(func(s *Status) { s.Error = "invoke result rejected: " + msg })
		if h.logUnknown != nil {
			h.logUnknown("node.invoke.result rejected", msg)
		}
	}
}

// handleInvoke dispatches a gateway->node command.
func (h *Host) handleInvoke(inv protocol.Invoke) protocol.InvokeResponse {
	h.setStatus(func(s *Status) {
		s.LastInvoke = fmt.Sprintf("%s at %s", inv.Command, time.Now().Format("15:04:05"))
	})

	switch inv.Command {
	case CmdSystemWhich:
		return h.reply(inv, h.systemWhich(inv.Params))
	case CmdFsListDir:
		return h.reply(inv, h.fsListDir(inv.Params))
	default:
		// Record the full request so an unimplemented command can be built against
		// real traffic rather than a guessed schema.
		if h.logUnknown != nil {
			h.logUnknown(inv.Command, string(inv.Params))
		}
		return h.fail(inv, fmt.Sprintf("ClawHQ does not implement %q", inv.Command))
	}
}

func (h *Host) reply(inv protocol.Invoke, payload any, err ...error) protocol.InvokeResponse {
	if len(err) > 0 && err[0] != nil {
		return h.fail(inv, err[0].Error())
	}
	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return h.fail(inv, marshalErr.Error())
	}
	return protocol.InvokeResponse{Type: "invoke-res", ID: inv.ID, OK: true, Payload: raw}
}

func (h *Host) fail(inv protocol.Invoke, message string) protocol.InvokeResponse {
	return protocol.InvokeResponse{
		Type:  "invoke-res",
		ID:    inv.ID,
		OK:    false,
		Error: &protocol.ErrorPayload{Code: "CLAWHQ_NODE_ERROR", Message: message},
	}
}

// systemWhich resolves binaries on PATH. Params: {"bins":["node","git"]}.
func (h *Host) systemWhich(raw json.RawMessage) any {
	var params struct {
		Bins []string `json:"bins"`
	}
	_ = json.Unmarshal(raw, &params)

	found := map[string]string{}
	for _, bin := range params.Bins {
		if path, err := exec.LookPath(bin); err == nil {
			found[bin] = path
		}
	}
	return map[string]any{"bins": found}
}

// fsListDir lists sub-directories of a shared folder. Params: {"path":"..."}.
//
// The response mirrors a real node host: {path, parent, home, entries:[{name,path,hidden}]}.
func (h *Host) fsListDir(raw json.RawMessage) any {
	var params struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &params)

	home, _ := os.UserHomeDir()

	// With no path, answer with the shared roots themselves. That gives the agent a
	// starting point without letting it discover anything outside them.
	if strings.TrimSpace(params.Path) == "" {
		entries := []map[string]any{}
		for _, folder := range h.shared() {
			entries = append(entries, map[string]any{
				"name": filepath.Base(folder),
				"path": folder,
			})
		}
		return map[string]any{"path": "", "parent": "", "home": home, "entries": entries}
	}

	target := filepath.Clean(params.Path)
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	if !h.isShared(target) {
		return map[string]any{
			"path":    target,
			"parent":  "",
			"home":    home,
			"entries": []any{},
			"error":   "path is outside the folders ClawHQ shares",
		}
	}

	dirEntries, err := os.ReadDir(target)
	if err != nil {
		return map[string]any{
			"path": target, "parent": filepath.Dir(target), "home": home,
			"entries": []any{}, "error": err.Error(),
		}
	}

	entries := []map[string]any{}
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue // a real node host lists directories only
		}
		entry := map[string]any{
			"name": e.Name(),
			"path": filepath.Join(target, e.Name()),
		}
		if strings.HasPrefix(e.Name(), ".") {
			entry["hidden"] = true
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i]["name"].(string) < entries[j]["name"].(string)
	})

	return map[string]any{
		"path":    target,
		"parent":  filepath.Dir(target),
		"home":    home,
		"entries": entries,
	}
}

// departmentsList reports the org chart: departments plus who sits in each.
func (h *Host) departmentsList() any {
	cfg := h.store.Read()
	departments := make([]map[string]any, 0, len(cfg.Departments))
	for _, d := range cfg.Departments {
		members := []string{}
		for agentID, deptID := range cfg.Assignments {
			if deptID == d.ID {
				members = append(members, agentID)
			}
		}
		sort.Strings(members)
		departments = append(departments, map[string]any{
			"id": d.ID, "name": d.Name, "emoji": d.Emoji, "order": d.Order, "agents": members,
		})
	}
	return map[string]any{"departments": departments}
}

// departmentCreate adds a department. The id is derived from the name so agents do not
// have to invent one, and creating an existing department updates it rather than
// failing — repeated calls from a retrying agent should be harmless.
func (h *Host) departmentCreate(raw json.RawMessage) (any, string) {
	var params struct {
		Name  string `json:"name"`
		Emoji string `json:"emoji"`
		ID    string `json:"id"`
	}
	_ = json.Unmarshal(raw, &params)

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, "a department needs a name"
	}
	id := strings.TrimSpace(params.ID)
	if id == "" {
		id = slugify(name)
	}
	if id == "" {
		return nil, fmt.Sprintf("could not derive an id from %q", name)
	}

	emoji := strings.TrimSpace(params.Emoji)
	if emoji == "" {
		emoji = "🏷️"
	}

	cfg, err := h.store.UpsertDepartment(store.Department{ID: id, Name: name, Emoji: emoji})
	if err != nil {
		return nil, err.Error()
	}
	return map[string]any{"id": id, "name": name, "emoji": emoji, "departments": len(cfg.Departments)}, ""
}

// agentAssign files an agent into a department. An empty departmentId unassigns.
func (h *Host) agentAssign(raw json.RawMessage) (any, string) {
	var params struct {
		AgentID      string `json:"agentId"`
		DepartmentID string `json:"departmentId"`
	}
	_ = json.Unmarshal(raw, &params)

	agentID := strings.TrimSpace(params.AgentID)
	if agentID == "" {
		return nil, "agentId is required"
	}
	deptID := strings.TrimSpace(params.DepartmentID)

	// Refuse unknown departments rather than silently creating a dangling assignment.
	if deptID != "" {
		known := false
		for _, d := range h.store.Read().Departments {
			if d.ID == deptID {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Sprintf("no department with id %q — create it first", deptID)
		}
	}

	if _, err := h.store.AssignAgent(agentID, deptID); err != nil {
		return nil, err.Error()
	}
	return map[string]any{"agentId": agentID, "departmentId": deptID}, ""
}

// slugify turns a display name into a stable id.
func slugify(name string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func (h *Host) shared() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]string(nil), h.sharedFolders...)
}

// isShared reports whether target sits inside one of the shared folders. Paths are
// compared after cleaning so "share/../../etc" cannot escape.
func (h *Host) isShared(target string) bool {
	for _, root := range h.shared() {
		rel, err := filepath.Rel(root, target)
		if err != nil {
			continue
		}
		if rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)) {
			return true
		}
	}
	return false
}

// ForgetPairing clears this node's device identity for the active gateway, so the next
// Start pairs anew and re-declares its command surface.
func (h *Host) ForgetPairing() error {
	gatewayID := h.Status().GatewayID
	h.Stop()
	store, err := h.identityStore(gatewayID)
	if err != nil {
		return err
	}
	return store.Reset()
}
