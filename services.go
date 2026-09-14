package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/surpriseawofemi/clawhq/internal/supervisor"
)

// ---------------------------------------------------------------------------
// GatewayService — connection, pairing, and raw RPC.
// ---------------------------------------------------------------------------

type GatewayService struct {
	conn  *gateway.Conn
	store *store.Store
}

func (s *GatewayService) Status() gateway.Status { return s.conn.Status() }

// Gateways lists the saved gateway profiles.
func (s *GatewayService) Gateways() []store.GatewayProfile { return s.store.Read().Gateways }

// HasPairing reports whether a gateway already has a device token.
func (s *GatewayService) HasPairing(gatewayID string) bool {
	return s.conn.HasStoredPairing(gatewayID)
}

// SaveGateway adds or updates a profile without connecting.
func (s *GatewayService) SaveGateway(g store.GatewayProfile) (store.Config, error) {
	return s.store.UpsertGateway(g)
}

func (s *GatewayService) RemoveGateway(gatewayID string) (store.Config, error) {
	// Drop the device identity too, so a re-added gateway pairs cleanly instead of
	// presenting a token the gateway may have already revoked.
	_ = s.conn.ForgetPairing(gatewayID)
	return s.store.RemoveGateway(gatewayID)
}

// ConnectWithToken pairs (or reconnects) using the gateway's shared auth token.
//
// Passing an empty token reuses the stored device token, which is the normal path once
// a gateway has been paired.
func (s *GatewayService) ConnectWithToken(ctx context.Context, gatewayID, name, url, token string) (gateway.Status, error) {
	cfg, err := s.store.UpsertGateway(s.profileFor(gatewayID, name, url))
	if err != nil {
		return s.conn.Status(), err
	}
	profile, _ := cfg.ActiveGateway()

	status, err := s.conn.Connect(ctx, profile.ID, profile.URL, gateway.Credential{Token: token})
	if err != nil {
		return status, err
	}
	if status.Phase == gateway.PhasePending {
		// Keep retrying in the background so approving elsewhere just works.
		s.conn.WaitForApproval(context.Background(), profile.ID, profile.URL, gateway.Credential{Token: token})
	}
	if status.Phase == gateway.PhaseConnected {
		_, _ = s.store.TouchGateway(profile.ID)
	}
	return status, nil
}

// PairWithSetupCode pairs using an `openclaw qr` code, which carries its own URL.
func (s *GatewayService) PairWithSetupCode(ctx context.Context, name, setupCode string) (gateway.Status, error) {
	sc, err := gateway.DecodeSetupCode(setupCode)
	if err != nil {
		return s.conn.Status(), err
	}
	cfg, err := s.store.UpsertGateway(s.profileFor("", name, sc.URL))
	if err != nil {
		return s.conn.Status(), err
	}
	profile, _ := cfg.ActiveGateway()

	cred := gateway.Credential{BootstrapToken: sc.BootstrapToken}
	status, err := s.conn.Connect(ctx, profile.ID, profile.URL, cred)
	if err != nil {
		return status, err
	}
	if status.Phase == gateway.PhasePending {
		s.conn.WaitForApproval(context.Background(), profile.ID, profile.URL, cred)
	}
	if status.Phase == gateway.PhaseConnected {
		_, _ = s.store.TouchGateway(profile.ID)
	}
	return status, nil
}

// profileFor picks the profile a connect attempt should run under. With no explicit
// ID it reuses the saved profile for that URL, so retrying a gateway — a second setup
// code, the token after a code — keeps the same device identity instead of minting a
// new device the operator has to approve all over again.
func (s *GatewayService) profileFor(gatewayID, name, url string) store.GatewayProfile {
	if gatewayID == "" {
		if existing, ok := s.store.Read().FindGatewayByURL(url); ok {
			gatewayID = existing.ID
			if name == "" {
				name = existing.Name
			}
		}
	}
	return store.GatewayProfile{ID: gatewayID, Name: name, URL: url}
}

// Connect switches to a saved gateway using its stored device token.
func (s *GatewayService) Connect(ctx context.Context, gatewayID string) (gateway.Status, error) {
	cfg := s.store.Read()
	if gatewayID == "" {
		if active, ok := cfg.ActiveGateway(); ok {
			gatewayID = active.ID
		}
	}
	var profile store.GatewayProfile
	for _, g := range cfg.Gateways {
		if g.ID == gatewayID {
			profile = g
			break
		}
	}
	if profile.ID == "" {
		return s.conn.Status(), fmt.Errorf("no saved gateway with id %q", gatewayID)
	}

	if _, err := s.store.SetActiveGateway(profile.ID); err != nil {
		return s.conn.Status(), err
	}
	status, err := s.conn.Connect(ctx, profile.ID, profile.URL, gateway.Credential{})
	if err != nil {
		return status, err
	}
	if status.Phase == gateway.PhaseConnected {
		_, _ = s.store.TouchGateway(profile.ID)
	}
	return status, nil
}

func (s *GatewayService) Disconnect() { s.conn.Disconnect() }

// CancelApprovalWait stops waiting for an operator to approve this device.
func (s *GatewayService) CancelApprovalWait() { s.conn.CancelApprovalWait() }

// ForgetPairing drops a gateway's device identity so the next connect pairs anew.
func (s *GatewayService) ForgetPairing(gatewayID string) error {
	return s.conn.ForgetPairing(gatewayID)
}

// Request performs a raw gateway RPC and returns the payload as a JSON string.
//
// Wails generates TS bindings from Go types, and the gateway's 424 methods have no
// shared Go shape, so the payload crosses as a string the frontend parses. That keeps
// one binding instead of hundreds of hand-written DTOs.
func (s *GatewayService) Request(ctx context.Context, method string, paramsJSON string) (string, error) {
	var params any
	if paramsJSON == "" {
		params = map[string]any{}
	} else if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return "", fmt.Errorf("invalid params for %s: %w", method, err)
	}

	raw, err := s.conn.Request(ctx, method, params)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "null", nil
	}
	return string(raw), nil
}

// SendChat posts a message; the reply streams back as `chat` events.
func (s *GatewayService) SendChat(ctx context.Context, sessionKey, message string) (string, error) {
	raw, err := s.conn.SendChat(ctx, sessionKey, message)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ---------------------------------------------------------------------------
// ConfigService — ClawHQ's own departments file.
// ---------------------------------------------------------------------------

type ConfigService struct {
	store *store.Store
}

func (s *ConfigService) Get() store.Config { return s.store.Read() }

func (s *ConfigService) Path() string { return s.store.Path() }

func (s *ConfigService) AssignAgent(agentID, departmentID string) (store.Config, error) {
	return s.store.AssignAgent(agentID, departmentID)
}

func (s *ConfigService) UpsertDepartment(dept store.Department) (store.Config, error) {
	return s.store.UpsertDepartment(dept)
}

func (s *ConfigService) RemoveDepartment(id string) (store.Config, error) {
	return s.store.RemoveDepartment(id)
}

// ---------------------------------------------------------------------------
// DaemonService — gateway process lifecycle.
// ---------------------------------------------------------------------------

type DaemonService struct{}

func (s *DaemonService) Status(ctx context.Context) supervisor.Status {
	return supervisor.Get(ctx)
}

func (s *DaemonService) Control(ctx context.Context, action string) (supervisor.Status, error) {
	return supervisor.Control(ctx, action)
}

func (s *DaemonService) MintSetupCode(ctx context.Context) (string, error) {
	return supervisor.MintSetupCode(ctx)
}

func (s *DaemonService) CLIAvailable(ctx context.Context) bool {
	return supervisor.Available(ctx)
}

// OpenPath reveals a file in the OS file manager or default handler.
func (s *DaemonService) OpenPath(path string) error {
	return supervisor.OpenPath(path)
}

// ---------------------------------------------------------------------------
// NodeService — ClawHQ's node role, which exposes this machine to agents.
// ---------------------------------------------------------------------------

type NodeService struct {
	host  *node.Host
	store *store.Store
	conn  *gateway.Conn
}

// RePair forgets this node's device identity so the next Enable pairs afresh.
//
// The gateway serves a node's command surface from its *approved* pairing record, and
// neither the node nor an operator can rewrite that in place — `node.pair.request` is
// refused both ways. Re-pairing is therefore how a changed command list reaches the
// gateway: it raises a new approval showing the new surface.
func (s *NodeService) RePair() (node.Status, error) {
	s.host.Disable()
	if err := s.host.ForgetPairing(); err != nil {
		return s.host.Status(), err
	}
	cfg := s.store.Read()
	if _, err := s.store.SetNodeConfig(store.NodeConfig{
		Enabled:        false,
		SharedFolders:  cfg.Node.SharedFolders,
		DesktopControl: cfg.Node.DesktopControl,
	}); err != nil {
		return s.host.Status(), err
	}
	return s.host.Status(), nil
}

// Status reports the live host state, with the persisted "enabled" flag overlaid so a
// node that is configured on but not yet connected reads as enabled rather than off.
func (s *NodeService) Status() node.Status {
	status := s.host.Status()
	if cfg := s.store.Read(); cfg.Node.Enabled {
		status.Enabled = true
	}
	return status
}

// Enable connects the node role. The gateway token is needed once, to pair; after
// that the stored device token is enough and token may be empty.
func (s *NodeService) Enable(ctx context.Context, token string) (node.Status, error) {
	cfg := s.store.Read()
	profile, ok := cfg.ActiveGateway()
	if !ok {
		return s.host.Status(), fmt.Errorf("connect a gateway before enabling the node role")
	}

	s.host.SetSharedFolders(cfg.Node.SharedFolders)
	s.host.SetDesktopControl(cfg.Node.DesktopControl)
	status, err := s.host.Start(ctx, profile.ID, profile.URL, token)
	if err != nil {
		return status, err
	}
	if _, err := s.store.SetNodeConfig(store.NodeConfig{
		Enabled:        true,
		SharedFolders:  cfg.Node.SharedFolders,
		DesktopControl: cfg.Node.DesktopControl,
	}); err != nil {
		return status, err
	}
	return status, nil
}

func (s *NodeService) Disable() (node.Status, error) {
	s.host.Disable()
	cfg := s.store.Read()
	if _, err := s.store.SetNodeConfig(store.NodeConfig{
		Enabled:        false,
		SharedFolders:  cfg.Node.SharedFolders,
		DesktopControl: cfg.Node.DesktopControl,
	}); err != nil {
		return s.host.Status(), err
	}
	return s.host.Status(), nil
}

// SetSharedFolders replaces the folders agents may browse on this machine.
func (s *NodeService) SetSharedFolders(folders []string) (node.Status, error) {
	cfg := s.store.Read()
	if _, err := s.store.SetNodeConfig(store.NodeConfig{
		Enabled:        cfg.Node.Enabled,
		SharedFolders:  folders,
		DesktopControl: cfg.Node.DesktopControl,
	}); err != nil {
		return s.host.Status(), err
	}
	s.host.SetSharedFolders(folders)
	return s.host.Status(), nil
}

// SetDesktopControl decides whether agents (and ClawHQ on another machine) may drive
// this computer's mouse and keyboard through computer.act. Off by default: it is the
// single most powerful thing the node role can expose.
func (s *NodeService) SetDesktopControl(on bool) (node.Status, error) {
	cfg := s.store.Read()
	if _, err := s.store.SetNodeConfig(store.NodeConfig{
		Enabled:        cfg.Node.Enabled,
		SharedFolders:  cfg.Node.SharedFolders,
		DesktopControl: on,
	}); err != nil {
		return s.host.Status(), err
	}
	s.host.SetDesktopControl(on)
	return s.host.Status(), nil
}
