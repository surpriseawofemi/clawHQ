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
	CmdSystemRunPrepare,
	CmdSystemRun,
	CmdExecApprovalsGet,
	CmdExecApprovalsSet,
	CmdFsListDir,
	CmdScreenSnapshot,
	CmdComputerAct,
	CmdSystemNotify,
	CmdDepartmentsList,
	CmdDepartmentCreate,
	CmdAgentAssign,
}

// CustomCommands are the verbs that are not in any gateway's default node allowlist.
// The operator side adds them to the gateway config so the descriptors register.
var CustomCommands = []string{CmdDepartmentsList, CmdDepartmentCreate, CmdAgentAssign, CmdFsListDir, CmdComputerAct}

// Caps ClawHQ claims. "file" covers directory browsing, "system" covers binary lookup
// and shell commands.
var Caps = []string{"file", "system", "screen", "computer", "notify"}

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
	// DesktopControl is whether computer.act invokes are honoured on this machine.
	DesktopControl bool `json:"desktopControl"`
	// Pairing is what the node is doing right now: "" (idle), "connecting",
	// "awaiting-approval", "reconnecting" or "connected".
	Pairing string `json:"pairing"`
	Error   string `json:"error"`
	// LastInvoke is a short human-readable trace of the most recent request, which is
	// the difference between "it silently does nothing" and a debuggable feature.
	LastInvoke string `json:"lastInvoke"`
	// Exec policy, mirrored so the settings panel has one source of truth.
	ExecMode  string   `json:"execMode"`
	ExecAllow []string `json:"execAllow"`
	// PendingExec are commands waiting for the user to allow or deny them.
	PendingExec []ExecRequest `json:"pendingExec"`
}

// Pairing states reported in Status.Pairing.
const (
	PairingIdle         = ""
	PairingConnecting   = "connecting"
	PairingAwaiting     = "awaiting-approval"
	PairingReconnecting = "reconnecting"
	PairingConnected    = "connected"
)

// Host is ClawHQ's node-role connection.
type Host struct {
	mu     sync.RWMutex
	client *ocgateway.Client
	status Status
	// generation is bumped by every Start and Stop, so a watcher for an old client
	// can tell a deliberate stop from a dropped connection.
	generation uint64

	identityRoot  string
	sharedFolders []string
	store         DepartmentStore

	desktopControl bool
	lastFrame      frameGeometry

	exec        execGate
	pendingExec map[string]ExecRequest

	emitStatus func(Status)
	// onNotify receives system.notify requests so the app can surface them.
	onNotify func(Notification)
	// logUnknown records invokes we do not implement yet, so the command surface can
	// be extended against real traffic instead of guesswork.
	logUnknown func(command string, params string)

	// Hooks below are optional and set by the app; see Hooks.
	onPending         func(gatewayID, deviceID string)
	onConnected       func(gatewayID, deviceID string)
	onExecRequest     func(ExecRequest)
	onExecRecord      func(ExecRecord)
	onAllowAlways     func(commandText string)
	onExecModeChanged func(mode string)
}

// Hooks let the app react to the node's life cycle without the node package knowing
// about the operator connection or the config store.
type Hooks struct {
	// OnPending fires when the gateway holds this node's pairing for approval; the
	// operator side approves it.
	OnPending func(gatewayID, deviceID string)
	// OnConnected fires after each successful connect.
	OnConnected func(gatewayID, deviceID string)
	// OnExecRequest fires when a command needs the user's decision.
	OnExecRequest func(ExecRequest)
	// OnExecRecord fires after every system.run, ran or refused, for the audit log.
	OnExecRecord func(ExecRecord)
	// OnAllowAlways fires when the user picked "always" for a command.
	OnAllowAlways func(commandText string)
	// OnExecModeChanged fires when the gateway pushes a new exec policy.
	OnExecModeChanged func(mode string)
}

func New(identityRoot string, st DepartmentStore, emitStatus func(Status), onNotify func(Notification), logUnknown func(string, string)) *Host {
	return &Host{
		identityRoot: identityRoot,
		store:        st,
		emitStatus:   emitStatus,
		onNotify:     onNotify,
		logUnknown:   logUnknown,
		status:       Status{Commands: commands, ExecMode: store.ExecAsk, ExecAllow: []string{}, PendingExec: []ExecRequest{}},
		exec:         execGate{mode: store.ExecAsk},
	}
}

// SetHooks installs the app's callbacks. Call before Start.
func (h *Host) SetHooks(hooks Hooks) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onPending = hooks.OnPending
	h.onConnected = hooks.OnConnected
	h.onExecRequest = hooks.OnExecRequest
	h.onExecRecord = hooks.OnExecRecord
	h.onAllowAlways = hooks.OnAllowAlways
	h.onExecModeChanged = hooks.OnExecModeChanged
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

// surfaceFile records the command list a pairing was approved with, so a changed
// surface can be detected and re-paired without the user noticing.
const surfaceFile = "surface.json"

// surfaceChanged reports whether the stored pairing was made with a different
// command list than the one compiled in.
func surfaceChanged(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, surfaceFile))
	if err != nil {
		return false // never recorded: nothing to compare against
	}
	var saved struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return true
	}
	return strings.Join(saved.Commands, ",") != strings.Join(commands, ",")
}

func saveSurface(dir string) {
	data, _ := json.Marshal(map[string]any{"commands": commands})
	_ = os.WriteFile(filepath.Join(dir, surfaceFile), data, 0o600)
}

// Start connects the node role.
//
// No credential is needed: a node that presents only its device identity is parked
// by the gateway as a pending pairing, which the operator side of ClawHQ approves.
// The shared token is still accepted for gateways configured to demand one. After
// the first pairing the stored device token is used. A pairing made with an older
// command list is dropped first, so the gateway re-records the current surface.
func (h *Host) Start(ctx context.Context, gatewayID, url, token string) (Status, error) {
	return h.start(ctx, gatewayID, url, token, PairingConnecting)
}

func (h *Host) start(ctx context.Context, gatewayID, url, token, phase string) (Status, error) {
	h.Stop()
	h.mu.Lock()
	h.generation++
	gen := h.generation
	h.mu.Unlock()

	store, err := h.identityStore(gatewayID)
	if err != nil {
		return h.Status(), fmt.Errorf("node identity store: %w", err)
	}
	dir := filepath.Join(h.identityRoot, gatewayIDOrDefault(gatewayID)+"-node")
	if strings.TrimSpace(store.LoadDeviceToken()) != "" && surfaceChanged(dir) {
		// The gateway serves a node's command surface from its approved pairing
		// record, and nothing can rewrite that in place. Pair afresh.
		_ = store.Reset()
	}
	id, err := store.LoadOrGenerate()
	if err != nil {
		return h.Status(), fmt.Errorf("node identity: %w", err)
	}

	h.setStatus(func(s *Status) {
		s.Enabled = true
		s.GatewayID = gatewayID
		s.DeviceID = id.DeviceID
		s.Pairing = phase
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
	deviceToken := strings.TrimSpace(store.LoadDeviceToken())
	switch {
	case strings.TrimSpace(token) != "":
		opts = append(opts, ocgateway.WithToken(token))
	case deviceToken != "":
		opts = append(opts, ocgateway.WithDeviceToken(deviceToken))
	default:
		// First contact: identity only. The gateway answers NOT_PAIRED and queues a
		// pairing request for an operator, which is what OnPending is for.
	}

	client := ocgateway.NewClient(opts...)
	if err := client.Connect(ctx, url); err != nil {
		if h.Status().Enabled && h.currentGeneration() == gen {
			if isAwaitingApproval(err) {
				h.setStatus(func(s *Status) {
					s.Connected = false
					s.Pairing = PairingAwaiting
					s.Error = ""
				})
				if h.onPending != nil {
					go h.onPending(gatewayID, id.DeviceID)
				}
			} else {
				h.setStatus(func(s *Status) { s.Connected = false; s.Error = err.Error() })
			}
			// Retry quietly until the gateway lets us in or the role is switched off,
			// whether that is an approval landing or a tunnel coming back.
			h.retryLater(gen, gatewayID, url, token)
		}
		return h.Status(), err
	}

	if hello := client.Hello(); hello != nil && hello.Auth != nil && hello.Auth.DeviceToken != "" {
		_ = store.SaveDeviceToken(hello.Auth.DeviceToken)
	}
	saveSurface(dir)

	h.mu.Lock()
	h.client = client
	h.mu.Unlock()

	h.setStatus(func(s *Status) { s.Connected = true; s.Pairing = PairingConnected; s.Error = "" })

	// Custom verbs reach agents as published tool descriptors, not as advertised
	// commands, so announce them once the session is live.
	h.publishPluginTools(ctx)

	go h.watch(client, gen, gatewayID, url, token)
	if h.onConnected != nil {
		go h.onConnected(gatewayID, id.DeviceID)
	}
	return h.Status(), nil
}

func gatewayIDOrDefault(id string) string {
	if id == "" {
		return "default"
	}
	return id
}

func (h *Host) currentGeneration() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.generation
}

// watch reconnects when the gateway drops the connection. A deliberate Stop bumps
// the generation first, so the watcher for that client simply exits.
func (h *Host) watch(client *ocgateway.Client, gen uint64, gatewayID, url, token string) {
	<-client.Done()
	if h.currentGeneration() != gen || !h.Status().Enabled {
		return
	}
	h.setStatus(func(s *Status) {
		s.Connected = false
		s.Pairing = PairingReconnecting
		s.Error = "connection to the gateway dropped, reconnecting"
	})
	h.retryLater(gen, gatewayID, url, token)
}

// retryLater re-runs start with backoff until it connects, the role is switched
// off, or a newer Start supersedes it.
func (h *Host) retryLater(gen uint64, gatewayID, url, token string) {
	go func() {
		delay := 3 * time.Second
		for {
			time.Sleep(delay)
			if h.currentGeneration() != gen || !h.Status().Enabled {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			// Keep the phase the UI is already showing; bouncing through
			// "connecting" every few seconds reads as flapping.
			phase := h.Status().Pairing
			if phase == PairingIdle || phase == PairingConnected {
				phase = PairingConnecting
			}
			next, _ := h.start(ctx, gatewayID, url, token, phase)
			cancel()
			if next.Connected {
				return
			}
			// start bumped the generation; follow it so this loop stays the owner.
			gen = h.currentGeneration()
			if delay < 30*time.Second {
				delay *= 2
			}
		}
	}()
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

func (h *Host) Stop() {
	h.mu.Lock()
	client := h.client
	h.client = nil
	h.generation++
	h.mu.Unlock()

	if client != nil {
		_ = client.Close()
	}
	h.setStatus(func(s *Status) { s.Connected = false; s.Pairing = PairingIdle })
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
	case CmdSystemRunPrepare:
		payload, failure = h.systemRunPrepare(params)
	case CmdSystemRun:
		payload, failure = h.systemRun(params)
	case CmdExecApprovalsGet:
		payload = h.execApprovalsGet()
	case CmdExecApprovalsSet:
		payload, failure = h.execApprovalsSet(params)
	case CmdFsListDir:
		payload = h.fsListDir(params)
	case CmdScreenSnapshot:
		payload, failure = h.screenSnapshot(params)
	case CmdComputerAct:
		payload, failure = h.computerAct(params)
	case CmdSystemNotify:
		payload, failure = h.systemNotify(params)
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
