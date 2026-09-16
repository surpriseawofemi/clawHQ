package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/surpriseawofemi/clawhq/internal/store"
)

// PluginStatus is what ClawHQ knows about its gateway plugin.
type PluginStatus struct {
	// Checked is false until the first look after a connect.
	Checked  bool     `json:"checked"`
	Present  bool     `json:"present"`
	Version  string   `json:"version"`
	Features []string `json:"features"`
	// Package is what to install on a gateway that lacks it.
	Package string `json:"package"`
	Error   string `json:"error"`
}

// pluginPackage is the ClawHub / npm name of the gateway plugin.
const pluginPackage = "clawhq-openclaw-plugin"

// pluginTools the node stops publishing when the gateway plugin provides them with
// the caller's real identity.
var pluginProvidedTools = []string{"clawhq_departments_list", "clawhq_department_create", "clawhq_agent_assign", "clawhq_ask_human"}

// pluginBridge is ClawHQ's side of the gateway plugin. When the plugin is there,
// the org chart lives on the gateway and the local copy is a mirror; notices come
// as gateway events instead of node relays; command history is pushed up as well
// as kept here. When it is not, nothing changes.
type pluginBridge struct {
	conn   *gateway.Conn
	store  *store.Store
	inbox  *store.Inbox
	host   *node.Host
	notify *notifier
	app    *application.App

	mu     sync.Mutex
	status PluginStatus
	// gatewayID the status was checked against; a switch resets it.
	gatewayID string
}

func newPluginBridge(conn *gateway.Conn, st *store.Store, inbox *store.Inbox, host *node.Host, notify *notifier) *pluginBridge {
	b := &pluginBridge{conn: conn, store: st, inbox: inbox, host: host, notify: notify, status: PluginStatus{Package: pluginPackage}}
	conn.AddEventListener(b.onGatewayEvent)
	return b
}

func (b *pluginBridge) Status() PluginStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

func (b *pluginBridge) present() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status.Present && b.conn.Status().Phase == gateway.PhaseConnected
}

func (b *pluginBridge) set(st PluginStatus) {
	st.Package = pluginPackage
	b.mu.Lock()
	b.status = st
	b.mu.Unlock()
	if b.app != nil {
		b.app.Event.Emit("plugin:status", st)
	}
}

func (b *pluginBridge) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return b.conn.Request(ctx, method, params)
}

// detect runs after every operator connect: is the plugin there, and if so, bring
// the local mirror up to date.
func (b *pluginBridge) detect(st gateway.Status) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := b.request(ctx, "clawhq.version", map[string]any{})
	if err != nil {
		// Not there (unknown method) or unreachable; either way, local mode.
		b.mu.Lock()
		b.gatewayID = st.GatewayID
		b.mu.Unlock()
		b.set(PluginStatus{Checked: true, Present: false})
		b.host.SetHiddenTools(ctx, nil)
		return
	}
	var v struct {
		Version  string   `json:"version"`
		Features []string `json:"features"`
	}
	_ = json.Unmarshal(raw, &v)
	b.mu.Lock()
	b.gatewayID = st.GatewayID
	b.mu.Unlock()
	b.set(PluginStatus{Checked: true, Present: true, Version: v.Version, Features: v.Features})
	log.Printf("plugin: clawhq %s on the gateway", v.Version)

	// The plugin's tools carry the real caller; the node's copies would only clash.
	b.host.SetHiddenTools(ctx, pluginProvidedTools)

	if err := b.syncOrg(ctx); err != nil {
		log.Printf("plugin: org sync: %v", err)
	}
	if err := b.catchUpInbox(ctx); err != nil {
		log.Printf("plugin: inbox catch-up: %v", err)
	}
}

type orgPayload struct {
	Departments []store.Department `json:"departments"`
	Assignments map[string]string  `json:"assignments"`
}

// syncOrg makes the gateway the owner of the org chart. A gateway that has none
// yet takes this machine's; after that the local copy mirrors the gateway.
func (b *pluginBridge) syncOrg(ctx context.Context) error {
	raw, err := b.request(ctx, "clawhq.org.get", map[string]any{})
	if err != nil {
		return err
	}
	var remote orgPayload
	if err := json.Unmarshal(raw, &remote); err != nil {
		return err
	}
	local := b.store.Read()
	if len(remote.Departments) == 0 && len(local.Departments) > 0 {
		raw, err = b.request(ctx, "clawhq.org.import", map[string]any{
			"departments": local.Departments,
			"assignments": local.Assignments,
		})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &remote); err != nil {
			return err
		}
		log.Printf("plugin: org chart moved to the gateway (%d departments)", len(remote.Departments))
	}
	return b.mirror(remote)
}

func (b *pluginBridge) mirror(remote orgPayload) error {
	cfg, err := b.store.ReplaceOrg(remote.Departments, remote.Assignments)
	if err != nil {
		return err
	}
	if b.app != nil {
		b.app.Event.Emit("config:changed", cfg)
	}
	return nil
}

// pullOrg refreshes the mirror, after a change made elsewhere.
func (b *pluginBridge) pullOrg() {
	if !b.present() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw, err := b.request(ctx, "clawhq.org.get", map[string]any{})
	if err != nil {
		return
	}
	var remote orgPayload
	if err := json.Unmarshal(raw, &remote); err == nil {
		_ = b.mirror(remote)
	}
}

// Write-through for the org: local first (the UI reads it), then the gateway.
// The gateway's answer is mirrored back so ids and ordering agree.
func (b *pluginBridge) orgWrite(method string, params map[string]any) {
	if !b.present() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		raw, err := b.request(ctx, method, params)
		if err != nil {
			log.Printf("plugin: %s: %v", method, err)
			return
		}
		var remote orgPayload
		if err := json.Unmarshal(raw, &remote); err == nil {
			_ = b.mirror(remote)
		}
	}()
}

func (b *pluginBridge) upsertDepartment(d store.Department) {
	b.orgWrite("clawhq.org.upsertDepartment", map[string]any{"id": d.ID, "name": d.Name, "emoji": d.Emoji, "order": d.Order})
}

func (b *pluginBridge) removeDepartment(id string) {
	b.orgWrite("clawhq.org.removeDepartment", map[string]any{"id": id})
}

func (b *pluginBridge) assign(agentID, departmentID string) {
	b.orgWrite("clawhq.org.assign", map[string]any{"agentId": agentID, "departmentId": departmentID})
}

// catchUpInbox fetches notices raised while this ClawHQ was away.
func (b *pluginBridge) catchUpInbox(ctx context.Context) error {
	var since int64
	have := map[string]bool{}
	for _, n := range b.inbox.List() {
		have[n.ID] = true
		if n.AtMs > since {
			since = n.AtMs
		}
	}
	raw, err := b.request(ctx, "clawhq.inbox.list", map[string]any{"sinceMs": since, "limit": 200})
	if err != nil {
		return err
	}
	var res struct {
		Notices []store.Notice `json:"notices"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	// Oldest first, so the inbox order matches when they happened.
	for i := len(res.Notices) - 1; i >= 0; i-- {
		n := res.Notices[i]
		if have[n.ID] {
			continue
		}
		n.Read = true // it was not for this sitting; keep it in history without a badge
		if n.Origin == "" {
			n.Origin = "gateway"
		}
		if _, err := b.inbox.Append(n); err != nil {
			log.Printf("plugin: inbox: %v", err)
		}
	}
	return nil
}

// appendExec pushes a command record to the gateway's shared history.
func (b *pluginBridge) appendExec(rec store.ExecRecord) {
	if !b.present() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		record := map[string]any{
			"id": rec.ID, "atMs": rec.AtMs, "machine": hostLabel(), "agentId": rec.AgentID, "sessionKey": rec.SessionKey,
			"command": rec.Command, "cwd": rec.Cwd, "decision": rec.Decision, "ran": rec.Ran, "success": rec.Success,
			"timedOut": rec.TimedOut, "durationMs": rec.DurationMs, "output": rec.Output, "error": rec.Error,
		}
		if rec.ExitCode != nil {
			record["exitCode"] = *rec.ExitCode
		}
		if _, err := b.request(ctx, "clawhq.exec.append", map[string]any{"record": record}); err != nil {
			log.Printf("plugin: exec append: %v", err)
		}
	}()
}

// install puts the plugin on the gateway from ClawHub. The gateway asks for consent
// to the plugin's declared capabilities with a review token in the error details;
// installing again with that token is the consent. The hook policy the plugin
// needs is patched into the gateway config first, and the gateway restarts itself
// to load the plugin; detect runs again on the reconnect.
func (b *pluginBridge) install() (PluginStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if err := b.patchHookPolicy(ctx); err != nil {
		return b.Status(), err
	}
	params := map[string]any{"source": "clawhub", "packageName": pluginPackage}
	_, err := b.request(ctx, "plugins.install", params)
	var rpcErr *gateway.RPCError
	if errors.As(err, &rpcErr) {
		if token := rpcErr.DetailString("reviewToken"); token != "" {
			params["acknowledgeCapabilities"] = map[string]any{"reviewToken": token}
			_, err = b.request(ctx, "plugins.install", params)
		}
	}
	if err != nil {
		return b.Status(), err
	}
	log.Printf("plugin: installed %s on the gateway; it restarts to load it", pluginPackage)
	return b.Status(), nil
}

// patchHookPolicy lets a non-bundled plugin use the conversation and prompt hooks.
func (b *pluginBridge) patchHookPolicy(ctx context.Context) error {
	raw, err := b.request(ctx, "config.get", map[string]any{})
	if err != nil {
		return err
	}
	var cur struct {
		Hash string `json:"hash"`
	}
	_ = json.Unmarshal(raw, &cur)
	patch := map[string]any{"plugins": map[string]any{"entries": map[string]any{"clawhq": map[string]any{
		"enabled": true,
		"hooks":   map[string]any{"allowConversationAccess": true, "allowPromptInjection": true},
	}}}}
	rawPatch, _ := json.Marshal(patch)
	_, err = b.request(ctx, "config.patch", map[string]any{"raw": string(rawPatch), "baseHash": cur.Hash, "note": "ClawHQ: plugin hook policy"})
	return err
}

// onGatewayEvent handles what the plugin broadcasts.
func (b *pluginBridge) onGatewayEvent(ev gateway.Event) {
	switch ev.Event {
	case "clawhq.notice":
		var n store.Notice
		if err := json.Unmarshal(ev.Payload, &n); err != nil || n.Body == "" {
			return
		}
		if n.Origin == "" {
			n.Origin = "gateway"
		}
		if n.AgentID != "" && n.AgentName == "" && b.notify != nil {
			n.AgentName, n.AgentEmoji = b.notify.agentIdentity(n.AgentID)
		}
		// Every ClawHQ gets this event, so no relay; the plugin already stored it.
		if b.notify != nil {
			go b.notify.show(n)
		}
	case "clawhq.org.changed":
		go b.pullOrg()
	}
}
