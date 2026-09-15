package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/surpriseawofemi/clawhq/internal/store"
)

// nodeAutopilot removes every manual step from the node role.
//
// Once the operator connection is up it makes sure the gateway allows ClawHQ's own
// verbs, starts the node, approves the node's pairing from the operator side, and
// re-pairs once if the gateway's approved surface is missing commands. The user only
// ever sees a switch in Settings.
type nodeAutopilot struct {
	conn  *gateway.Conn
	host  *node.Host
	store *store.Store

	mu       sync.Mutex
	starting bool
	// repaired remembers which gateways were re-paired this launch, so a gateway that
	// refuses part of the surface cannot trap the node in a pairing loop.
	repaired map[string]bool
}

func newNodeAutopilot(conn *gateway.Conn, host *node.Host, st *store.Store) *nodeAutopilot {
	a := &nodeAutopilot{conn: conn, host: host, store: st, repaired: map[string]bool{}}
	conn.SetOnConnected(a.onOperatorConnected)
	conn.AddEventListener(a.onGatewayEvent)
	return a
}

// onOperatorConnected runs after every operator connect, reconnects included.
func (a *nodeAutopilot) onOperatorConnected(st gateway.Status) {
	if !a.store.Read().Node.Enabled {
		return
	}
	go a.ensure()
}

// ensure brings the node role up if it is enabled and not already connected.
func (a *nodeAutopilot) ensure() {
	a.mu.Lock()
	if a.starting {
		a.mu.Unlock()
		return
	}
	a.starting = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.starting = false
		a.mu.Unlock()
	}()

	cfg := a.store.Read()
	if !cfg.Node.Enabled {
		return
	}
	profile, ok := cfg.ActiveGateway()
	if !ok || a.conn.Status().Phase != gateway.PhaseConnected {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := a.ensureAllowlist(ctx); err != nil {
		log.Printf("autopilot: gateway allowlist: %v", err)
	}

	ns := a.host.Status()
	if ns.Connected && ns.GatewayID == profile.ID {
		return
	}
	a.host.SetSharedFolders(cfg.Node.SharedFolders)
	a.host.SetDesktopControl(cfg.Node.DesktopControl)
	a.host.SetExecPolicy(cfg.Node.Exec.Mode, cfg.Node.Exec.Allow, cfg.Node.Exec.Agents)
	if _, err := a.host.Start(ctx, profile.ID, profile.URL, ""); err != nil {
		// Pending approval and transient failures are handled by the host's own
		// retry loop; this is just for the log.
		log.Printf("autopilot: node start: %v", err)
	}
}

// ensureAllowlist adds every command ClawHQ advertises to the gateway's node command
// allowlist, so the pairing record and the published tool descriptors carry the full
// surface. Returns whether the config was changed.
//
// The key moved from gateway.nodes.allowCommands to gateway.nodes.commands.allow
// between gateway releases; whichever the gateway already has wins, and a patch the
// gateway rejects is retried with the other spelling.
func (a *nodeAutopilot) ensureAllowlist(ctx context.Context) (bool, error) {
	raw, err := a.conn.Request(ctx, "config.get", nil)
	if err != nil {
		return false, err
	}
	var snap struct {
		Config map[string]any `json:"config"`
		Hash   string         `json:"hash"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return false, fmt.Errorf("config.get: %w", err)
	}

	existing, nested := currentAllowlist(snap.Config)
	have := map[string]bool{}
	for _, c := range existing {
		have[c] = true
	}
	merged := append([]string(nil), existing...)
	for _, c := range node.Commands() {
		if !have[c] {
			merged = append(merged, c)
		}
	}
	if len(merged) == len(existing) {
		return false, nil
	}

	patch := func(nestedKey bool) error {
		var body map[string]any
		if nestedKey {
			body = map[string]any{"gateway": map[string]any{"nodes": map[string]any{"commands": map[string]any{"allow": merged}}}}
		} else {
			body = map[string]any{"gateway": map[string]any{"nodes": map[string]any{"allowCommands": merged}}}
		}
		rawPatch, _ := json.Marshal(body)
		_, err := a.conn.Request(ctx, "config.patch", map[string]any{
			"raw":      string(rawPatch),
			"baseHash": snap.Hash,
			"note":     "ClawHQ: allow this node's commands",
		})
		return err
	}
	if err := patch(nested); err != nil {
		if err2 := patch(!nested); err2 != nil {
			return false, err
		}
	}
	log.Printf("autopilot: added %d node commands to the gateway allowlist", len(merged)-len(existing))
	return true, nil
}

// currentAllowlist reads whichever allowlist key the gateway config holds. The bool
// reports the nested (newer) spelling.
func currentAllowlist(cfg map[string]any) ([]string, bool) {
	gw, _ := cfg["gateway"].(map[string]any)
	nodes, _ := gw["nodes"].(map[string]any)
	if cmds, ok := nodes["commands"].(map[string]any); ok {
		return toStrings(cmds["allow"]), true
	}
	if v, ok := nodes["allowCommands"]; ok {
		return toStrings(v), false
	}
	return nil, true
}

func toStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pendingRow is one row of device.pair.list or node.pair.list. Field names are
// defensive: the two queues and their events spell the identity differently.
type pendingRow struct {
	RequestID string   `json:"requestId"`
	ID        string   `json:"id"`
	DeviceID  string   `json:"deviceId"`
	NodeID    string   `json:"nodeId"`
	Role      string   `json:"role"`
	Roles     []string `json:"roles"`
}

func (p pendingRow) requestID() string {
	if p.RequestID != "" {
		return p.RequestID
	}
	return p.ID
}

func (p pendingRow) matches(deviceID string) bool {
	return deviceID != "" && (p.DeviceID == deviceID || p.NodeID == deviceID)
}

// pairingQueues lists the pending queues to search, with their approve RPCs. A
// node-role connection with only a device identity is parked in the *device* queue
// (the refusal reads "device is not approved yet"); the node queue is the older
// node.pair.request path, checked too in case a gateway uses it.
var pairingQueues = []struct{ list, approve string }{
	{"device.pair.list", "device.pair.approve"},
	{"node.pair.list", "node.pair.approve"},
}

// approveOnce searches every queue for this device and approves the first match.
// Returns whether something was approved.
func (a *nodeAutopilot) approveOnce(ctx context.Context, deviceID string) bool {
	for _, q := range pairingQueues {
		raw, err := a.conn.Request(ctx, q.list, nil)
		if err != nil {
			log.Printf("autopilot: %s: %v", q.list, err)
			continue
		}
		var res struct {
			Pending []pendingRow `json:"pending"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			log.Printf("autopilot: %s: unexpected payload %.200s", q.list, string(raw))
			continue
		}
		for _, p := range res.Pending {
			if !p.matches(deviceID) || p.requestID() == "" {
				continue
			}
			if _, err := a.conn.Request(ctx, q.approve, map[string]any{"requestId": p.requestID()}); err != nil {
				log.Printf("autopilot: %s: %v", q.approve, err)
				return false
			}
			log.Printf("autopilot: approved this machine's node pairing via %s", q.approve)
			return true
		}
	}
	return false
}

// approvePairing approves this app's own node from the operator side. It polls for
// a while because the pairing request can land on the gateway a beat after the node
// is told to wait.
func (a *nodeAutopilot) approvePairing(gatewayID, deviceID string) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if a.conn.Status().Phase != gateway.PhaseConnected {
			time.Sleep(3 * time.Second)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		approved := a.approveOnce(ctx, deviceID)
		cancel()
		if approved || a.host.Status().Connected {
			return
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("autopilot: gave up looking for this node's pairing request")
}

// onGatewayEvent approves the node's pairing the moment the gateway announces it,
// which is faster than the poll in approvePairing.
func (a *nodeAutopilot) onGatewayEvent(ev gateway.Event) {
	var approve string
	switch ev.Event {
	case "device.pair.requested":
		approve = "device.pair.approve"
	case "node.pair.requested":
		approve = "node.pair.approve"
	default:
		return
	}
	var req pendingRow
	if err := json.Unmarshal(ev.Payload, &req); err != nil || req.requestID() == "" {
		return
	}
	if !req.matches(a.host.Status().DeviceID) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := a.conn.Request(ctx, approve, map[string]any{"requestId": req.requestID()}); err != nil {
			log.Printf("autopilot: %s: %v", approve, err)
			return
		}
		log.Printf("autopilot: approved this machine's node pairing via event")
	}()
}

// verifySurface checks, once per gateway per launch, that the gateway's approved
// command list for this node matches what ClawHQ advertises. If it does not, the
// pairing was approved before the allowlist was extended, and only a fresh pairing
// records the full surface.
func (a *nodeAutopilot) verifySurface(gatewayID, deviceID string) {
	time.Sleep(2 * time.Second)
	a.mu.Lock()
	done := a.repaired[gatewayID]
	a.mu.Unlock()
	if done {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := a.conn.Request(ctx, "node.list", nil)
	if err != nil {
		return
	}
	var res struct {
		Paired []struct {
			NodeID   string   `json:"nodeId"`
			Commands []string `json:"commands"`
		} `json:"paired"`
		Nodes []struct {
			NodeID   string   `json:"nodeId"`
			Commands []string `json:"commands"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return
	}
	rows := res.Paired
	if len(rows) == 0 {
		rows = res.Nodes
	}
	var approved []string
	found := false
	for _, row := range rows {
		if row.NodeID == deviceID {
			approved = row.Commands
			found = true
			break
		}
	}
	if !found {
		return
	}
	have := map[string]bool{}
	for _, c := range approved {
		have[c] = true
	}
	missing := 0
	for _, c := range node.Commands() {
		if !have[c] {
			missing++
		}
	}
	if missing == 0 {
		return
	}

	a.mu.Lock()
	a.repaired[gatewayID] = true
	a.mu.Unlock()
	log.Printf("autopilot: gateway approved %d of %d node commands, re-pairing", len(node.Commands())-missing, len(node.Commands()))
	if _, err := a.ensureAllowlist(ctx); err != nil {
		log.Printf("autopilot: gateway allowlist: %v", err)
		return
	}
	if err := a.host.ForgetPairing(); err != nil {
		log.Printf("autopilot: forget pairing: %v", err)
		return
	}
	a.ensure()
}

// forgetNode removes an old pairing of this machine from the gateway, so a node
// that re-paired under a fresh identity does not leave a ghost in the desktops list.
// It waits for the operator connection when it has to.
func (a *nodeAutopilot) forgetNode(gatewayID, oldID string) {
	if oldID == "" {
		return
	}
	for attempt := 0; attempt < 12; attempt++ {
		st := a.conn.Status()
		if st.Phase == gateway.PhaseConnected && st.GatewayID == gatewayID {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := a.conn.Request(ctx, "node.pair.remove", map[string]any{"nodeId": oldID})
			if err != nil {
				_, err = a.conn.Request(ctx, "device.pair.remove", map[string]any{"deviceId": oldID})
			}
			cancel()
			if err == nil {
				log.Printf("autopilot: removed this machine's old node pairing %s", oldID[:min(12, len(oldID))])
			} else {
				log.Printf("autopilot: old node pairing %s not removed: %v", oldID[:min(12, len(oldID))], err)
			}
			return
		}
		time.Sleep(5 * time.Second)
	}
}

// rePair drops the node identity and pairs again, keeping the role on.
func (a *nodeAutopilot) rePair() error {
	if err := a.host.ForgetPairing(); err != nil {
		return err
	}
	go a.ensure()
	return nil
}
