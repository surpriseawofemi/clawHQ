package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"strconv"
	"strings"
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
	// Latest is the newest version on ClawHub, once looked up.
	Latest          string `json:"latest"`
	UpdateAvailable bool   `json:"updateAvailable"`
	// Upgrading names the step in flight: uninstalling, installing, enabling; "" when idle.
	Upgrading string `json:"upgrading"`
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
	// latest is the newest ClawHub version seen; upgrading is the step in flight.
	latest    string
	upgrading string

	// claude runs server tasks agents hand over through the plugin.
	claude  *ClaudeService
	taskMu  sync.Mutex
	tasks   map[string]bool
	regLoop bool
}

func newPluginBridge(conn *gateway.Conn, st *store.Store, inbox *store.Inbox, host *node.Host, notify *notifier) *pluginBridge {
	b := &pluginBridge{conn: conn, store: st, inbox: inbox, host: host, notify: notify, status: PluginStatus{Package: pluginPackage}}
	conn.AddEventListener(b.onGatewayEvent)
	return b
}

func (b *pluginBridge) Status() PluginStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.compose(b.status)
}

// compose adds the update bookkeeping to a status snapshot. Caller holds b.mu.
func (b *pluginBridge) compose(st PluginStatus) PluginStatus {
	st.Package = pluginPackage
	st.Latest = b.latest
	st.UpdateAvailable = st.Present && b.latest != "" && semverLess(st.Version, b.latest)
	st.Upgrading = b.upgrading
	return st
}

func (b *pluginBridge) present() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status.Present && b.conn.Status().Phase == gateway.PhaseConnected
}

func (b *pluginBridge) set(st PluginStatus) {
	b.mu.Lock()
	b.status = st
	out := b.compose(st)
	b.mu.Unlock()
	if b.app != nil {
		b.app.Event.Emit("plugin:status", out)
	}
}

// announce re-emits the status after the update bookkeeping changed.
func (b *pluginBridge) announce() {
	b.mu.Lock()
	out := b.compose(b.status)
	b.mu.Unlock()
	if b.app != nil {
		b.app.Event.Emit("plugin:status", out)
	}
}

// semverLess reports whether a is an older version than b (x.y.z, numeric parts).
func semverLess(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func (b *pluginBridge) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return b.conn.Request(ctx, method, params)
}

// detect runs after every operator connect: is the plugin there, and if so, bring
// the local mirror up to date.
func (b *pluginBridge) detect(st gateway.Status) {
	b.mu.Lock()
	busy := b.upgrading != ""
	b.mu.Unlock()
	if busy {
		// The upgrade goroutine owns the connection until it is done.
		return
	}
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
	go b.checkLatest()
	go b.registerServers()
	go b.catchUpServerTasks()
	b.keepRegistering()
	// A gateway with a tool profile must list the plugin's tools or agents never see them.
	if err := b.patchToolAllow(ctx); err != nil {
		log.Printf("plugin: tool allowlist: %v", err)
	}
}

// checkLatest asks ClawHub (through the gateway's plugin search) for the newest
// version. With auto-update on, a newer one is installed straight away.
func (b *pluginBridge) checkLatest() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := b.request(ctx, "plugins.search", map[string]any{"query": pluginPackage, "limit": 5})
	if err != nil {
		return
	}
	var res struct {
		Results []struct {
			Package struct {
				Name          string `json:"name"`
				LatestVersion string `json:"latestVersion"`
			} `json:"package"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return
	}
	latest := ""
	for _, r := range res.Results {
		if r.Package.Name == pluginPackage {
			latest = r.Package.LatestVersion
		}
	}
	if latest == "" {
		return
	}
	b.mu.Lock()
	b.latest = latest
	current := b.status.Version
	b.mu.Unlock()
	b.announce()
	if semverLess(current, latest) {
		log.Printf("plugin: clawhq %s is on ClawHub, %s installed", latest, current)
		if b.store.Read().AutoUpdate {
			go b.upgrade()
		}
	}
}

// upgrade replaces the plugin with the newest ClawHub version. The gateway has no
// in-place update for plugins, so this is uninstall, install (with capability
// consent), enable; the gateway restarts itself after each of the first two and
// this waits for it to come back before the next step.
func (b *pluginBridge) upgrade() error {
	b.mu.Lock()
	if b.upgrading != "" {
		b.mu.Unlock()
		return errors.New("an update is already in progress")
	}
	target := b.latest
	b.upgrading = "uninstalling"
	b.mu.Unlock()
	b.announce()
	finish := func(err error) error {
		b.mu.Lock()
		b.upgrading = ""
		b.mu.Unlock()
		if err != nil {
			log.Printf("plugin: update failed: %v", err)
		}
		b.announce()
		// A fresh look, whatever happened.
		go b.detect(b.conn.Status())
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Every ClawHQ on this gateway sees the same new version at about the same
	// time. Without this two of them race: one installs while the other's
	// uninstall removes the directory underneath it, and the gateway comes back
	// with no plugin at all. So: stagger, then look at what is actually installed
	// right before touching anything.
	time.Sleep(time.Duration(rand.IntN(45)) * time.Second)
	installed, present := b.installedVersion(ctx)
	if present && !semverLess(installed, target) {
		log.Printf("plugin: clawhq %s already installed (another ClawHQ got there first)", installed)
		return finish(nil)
	}
	if present {
		if _, err := b.request(ctx, "plugins.uninstall", map[string]any{"pluginId": "clawhq"}); err != nil {
			return finish(err)
		}
		if err := b.waitForGateway(ctx); err != nil {
			log.Printf("plugin: %v (the gateway defers restarts while agents run); carrying on", err)
		}
	}
	b.setStage("installing")
	if err := b.installFromHub(ctx, target); err != nil {
		return finish(err)
	}
	b.setStage("enabling")
	// The install removed the plugin's settings; put them back now, so they are
	// on disk whenever the gateway gets round to restarting.
	if err := b.patchHookPolicy(ctx); err != nil {
		log.Printf("plugin: hook policy not patched yet: %v", err)
	}
	if err := b.patchToolAllow(ctx); err != nil {
		log.Printf("plugin: tool allowlist not patched yet: %v", err)
	}
	if err := b.waitForGateway(ctx); err != nil {
		log.Printf("plugin: %v; the plugin loads on the gateway's next restart", err)
	}
	if err := b.patchHookPolicy(ctx); err != nil {
		return finish(err)
	}
	if err := b.patchToolAllow(ctx); err != nil {
		log.Printf("plugin: tool allowlist: %v", err)
	}
	log.Printf("plugin: updated to clawhq %s", target)
	return finish(nil)
}

// installedVersion asks the gateway's plugin list, which is the truth even while
// the plugin's methods are not loaded yet (a restart is pending).
func (b *pluginBridge) installedVersion(ctx context.Context) (version string, present bool) {
	raw, err := b.request(ctx, "plugins.list", map[string]any{})
	if err != nil {
		return "", false
	}
	var res struct {
		Plugins []struct {
			ID        string `json:"id"`
			Version   string `json:"version"`
			Installed bool   `json:"installed"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", false
	}
	for _, p := range res.Plugins {
		if p.ID == "clawhq" && p.Installed {
			return p.Version, true
		}
	}
	return "", false
}

func (b *pluginBridge) setStage(stage string) {
	b.mu.Lock()
	b.upgrading = stage
	b.mu.Unlock()
	b.announce()
}

// waitForGateway rides out the gateway's restart: first the connection drops,
// then it comes back and answers health.
func (b *pluginBridge) waitForGateway(ctx context.Context) error {
	time.Sleep(8 * time.Second)
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if b.conn.Status().Phase == gateway.PhaseConnected {
			hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := b.request(hctx, "health", map[string]any{})
			cancel()
			if err == nil {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	return errors.New("the gateway did not come back within four minutes")
}

// installFromHub installs one version, answering the capability-consent challenge.
func (b *pluginBridge) installFromHub(ctx context.Context, version string) error {
	params := map[string]any{"source": "clawhub", "packageName": pluginPackage}
	if version != "" {
		params["version"] = version
	}
	_, err := b.request(ctx, "plugins.install", params)
	var rpcErr *gateway.RPCError
	if errors.As(err, &rpcErr) {
		if token := rpcErr.DetailString("reviewToken"); token != "" {
			params["acknowledgeCapabilities"] = map[string]any{"reviewToken": token}
			_, err = b.request(ctx, "plugins.install", params)
		}
	}
	return err
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

	if err := b.patchToolAllow(ctx); err != nil {
		log.Printf("plugin: tool allowlist: %v", err)
	}
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

// pluginTools is every agent tool the plugin registers (its manifest's
// contracts.tools), plus the plugin group. A gateway running a tool profile only offers agents the tools
// in tools.allow / tools.alsoAllow, so these have to be listed or agents silently
// lack them.
var pluginTools = []string{
	// The plugin group entry matters for agents on a CLI backend (Claude Code,
	// Codex): OpenClaw only starts its MCP tool bridge for a run when the
	// allowlist names a plugin or the plugin group, so exact tool names alone
	// leave those agents without any plugin tool.
	"group:plugins",
	"clawhq_departments_list", "clawhq_department_create", "clawhq_agent_assign", "clawhq_ask_human",
	"clawhq_tasks_list", "clawhq_task_create", "clawhq_task_update",
	"clawhq_team_post", "clawhq_team_read",
	"clawhq_issue_create", "clawhq_issues_list", "clawhq_issue_update",
}

// patchToolAllow adds any missing plugin tool to tools.alsoAllow. A gateway with
// no tool profile offers everything and needs nothing; one with a profile gets
// the list extended, never shortened.
func (b *pluginBridge) patchToolAllow(ctx context.Context) error {
	raw, err := b.request(ctx, "config.get", map[string]any{})
	if err != nil {
		return err
	}
	var cur struct {
		Hash   string `json:"hash"`
		Config struct {
			Tools struct {
				Profile   string   `json:"profile"`
				AlsoAllow []string `json:"alsoAllow"`
				Allow     []string `json:"allow"`
			} `json:"tools"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &cur); err != nil {
		return err
	}
	if cur.Config.Tools.Profile == "" && len(cur.Config.Tools.Allow) == 0 {
		return nil // no profile: every tool is offered already
	}
	have := map[string]bool{}
	for _, t := range cur.Config.Tools.AlsoAllow {
		have[t] = true
	}
	for _, t := range cur.Config.Tools.Allow {
		have[t] = true
	}
	next := append([]string{}, cur.Config.Tools.AlsoAllow...)
	missing := 0
	for _, t := range pluginTools {
		if !have[t] {
			next = append(next, t)
			missing++
		}
	}
	if missing == 0 {
		return nil
	}
	patch := map[string]any{"tools": map[string]any{"alsoAllow": next}}
	rawPatch, _ := json.Marshal(patch)
	_, err = b.request(ctx, "config.patch", map[string]any{"raw": string(rawPatch), "baseHash": cur.Hash, "note": "ClawHQ: allow every ClawHQ plugin tool"})
	if err == nil {
		log.Printf("plugin: %d ClawHQ tools added to the gateway's tool allowlist", missing)
	}
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
	case "clawhq.server.task":
		var t struct {
			ID       string `json:"id"`
			ServerID string `json:"serverId"`
			ClawhqID string `json:"clawhqId"`
		}
		if json.Unmarshal(ev.Payload, &t) == nil && t.ID != "" {
			go b.workServerTask(t.ID, t.ServerID)
		}
	case "clawhq.org.changed":
		go b.pullOrg()
	}
}

// ---- servers handed to agents ------------------------------------------------

// registerServers tells the plugin which servers this ClawHQ can work on: names
// and projects only, never credentials.
func (b *pluginBridge) registerServers() {
	cfg := b.store.Read()
	type proj struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Agent string `json:"agent"`
	}
	type srv struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description,omitempty"`
		Agents      []string `json:"agents,omitempty"`
		Projects    []proj   `json:"projects"`
	}
	list := []srv{}
	for _, sv := range cfg.Servers {
		// Isolated servers and servers that take no tasks stay unknown to agents.
		if sv.Isolated || sv.TasksOff {
			continue
		}
		e := srv{ID: sv.ID, Name: sv.Name, Description: sv.Description, Agents: sv.TaskAgents, Projects: []proj{}}
		for _, pr := range sv.Projects {
			e.Projects = append(e.Projects, proj{ID: pr.ID, Name: pr.Name, Agent: pr.Agent})
		}
		list = append(list, e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := b.request(ctx, "clawhq.servers.register", map[string]any{"clawhqId": b.store.EnsureInstanceID(), "servers": list}); err != nil {
		log.Printf("plugin: servers not registered: %v", err)
	}
}

// keepRegistering re-registers the server list every ten minutes while the plugin
// is present, so added projects and renamed servers reach the agents.
func (b *pluginBridge) keepRegistering() {
	b.mu.Lock()
	if b.regLoop {
		b.mu.Unlock()
		return
	}
	b.regLoop = true
	b.mu.Unlock()
	go func() {
		for range time.Tick(10 * time.Minute) {
			b.mu.Lock()
			present := b.status.Present
			b.mu.Unlock()
			if present {
				b.registerServers()
			}
		}
	}()
}

// ownsServer says whether this ClawHQ may run tasks on the server: it is saved
// here, not isolated, and open to agent tasks.
func (b *pluginBridge) ownsServer(id string) bool {
	for _, sv := range b.store.Read().Servers {
		if sv.ID == id {
			return !sv.Isolated && !sv.TasksOff
		}
	}
	return false
}

// allowsAgent says whether the server takes tasks from this agent.
func (b *pluginBridge) allowsAgent(serverID, agent string) bool {
	for _, sv := range b.store.Read().Servers {
		if sv.ID != serverID {
			continue
		}
		if len(sv.TaskAgents) == 0 {
			return true
		}
		for _, a := range sv.TaskAgents {
			if strings.EqualFold(a, agent) {
				return true
			}
		}
	}
	return false
}

// catchUpServerTasks runs tasks queued while this ClawHQ was away.
func (b *pluginBridge) catchUpServerTasks() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := b.request(ctx, "clawhq.server.tasks.list", map[string]any{"status": "queued", "limit": 50})
	if err != nil {
		return
	}
	var res struct {
		Tasks []struct {
			ID       string `json:"id"`
			ServerID string `json:"serverId"`
		} `json:"tasks"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return
	}
	for _, t := range res.Tasks {
		go b.workServerTask(t.ID, t.ServerID)
	}
}

// workServerTask claims a task for a server this ClawHQ owns, runs it, and posts
// the result back; the plugin returns it to the agent that asked.
func (b *pluginBridge) workServerTask(taskID, serverID string) {
	if b.claude == nil || !b.ownsServer(serverID) {
		return
	}
	b.taskMu.Lock()
	if b.tasks == nil {
		b.tasks = map[string]bool{}
	}
	if b.tasks[taskID] {
		b.taskMu.Unlock()
		return
	}
	b.tasks[taskID] = true
	b.taskMu.Unlock()
	defer func() {
		b.taskMu.Lock()
		delete(b.tasks, taskID)
		b.taskMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	raw, err := b.request(ctx, "clawhq.server.task.claim", map[string]any{"id": taskID, "clawhqId": b.store.EnsureInstanceID()})
	if err != nil {
		log.Printf("plugin: task %s not claimed: %v", taskID, err)
		return
	}
	var claim struct {
		OK   bool `json:"ok"`
		Task struct {
			ProjectID string `json:"projectId"`
			Task      string `json:"task"`
			From      string `json:"from"`
		} `json:"task"`
	}
	if json.Unmarshal(raw, &claim) != nil || !claim.OK {
		return
	}
	if !b.allowsAgent(serverID, claim.Task.From) {
		rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rcancel()
		_, _ = b.request(rctx, "clawhq.server.task.result", map[string]any{"id": taskID, "ok": false, "text": "this server does not accept tasks from " + claim.Task.From})
		return
	}
	prompt := "Task handed over by the OpenClaw agent \"" + claim.Task.From + "\" through ClawHQ. Do it fully in this project, then reply with a short report of what changed and anything that still needs a decision.\n\n" + claim.Task.Task
	out, err := b.claude.RunTask(ctx, serverID, claim.Task.ProjectID, prompt)
	text, ok, cost := out.Text, out.OK, out.CostUsd
	if err != nil {
		text, ok = err.Error(), false
	} else if !ok && out.Err != "" {
		text = out.Err
	}
	rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer rcancel()
	if _, err := b.request(rctx, "clawhq.server.task.result", map[string]any{"id": taskID, "ok": ok, "text": text, "costUsd": cost}); err != nil {
		log.Printf("plugin: task %s result not posted: %v", taskID, err)
	}
	if b.notify != nil {
		body := strings.TrimSpace(text)
		if len(body) > 240 {
			body = body[:240] + "…"
		}
		title := "Server task done"
		if !ok {
			title = "Server task failed"
		}
		b.notify.show(store.Notice{Title: title + " · for " + claim.Task.From, Body: body, AgentName: "Claude Code", AgentEmoji: "🧑‍💻", Origin: "server:" + serverID, AtMs: time.Now().UnixMilli()})
	}
}
