// Package gateway owns ClawHQ's connection to an OpenClaw Gateway.
//
// Authentication is device-based. A client that presents only a credential gets an
// empty scope array back and every RPC then fails with "missing scope: operator.read";
// a client that presents the same credential *alongside a signed Ed25519 device
// identity* is granted scopes and issued a device token for later reconnects. So
// ClawHQ keeps a persistent identity per gateway and pairs once.
//
// Either credential works for that first pairing: the gateway's shared auth token, or
// a one-time bootstrap token from an `openclaw qr` setup code.
package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ocgateway "github.com/a3tai/openclaw-go/gateway"
	"github.com/a3tai/openclaw-go/identity"
	"github.com/a3tai/openclaw-go/protocol"
)

// Scopes ClawHQ requests.
//
// operator.admin is what makes agent editing possible; operator.pairing is what lets
// ClawHQ approve pending devices and nodes — including its own node role, whose
// pairing updates the gateway requires an operator to bless.
var Scopes = []protocol.Scope{
	protocol.ScopeOperatorRead,
	protocol.ScopeOperatorWrite,
	protocol.ScopeOperatorAdmin,
	protocol.ScopeOperatorPairing,
}

const DefaultURL = "ws://127.0.0.1:18789"

// Connection phases. "pending" means the gateway accepted the request but an operator
// still has to approve this device before scopes are granted.
const (
	PhaseIdle       = "idle"
	PhaseConnecting = "connecting"
	PhaseConnected  = "connected"
	PhasePending    = "pending"
	PhaseError      = "error"
)

type Status struct {
	Phase         string   `json:"phase"`
	GatewayID     string   `json:"gatewayId"`
	URL           string   `json:"url"`
	Scopes        []string `json:"scopes"`
	DeviceID      string   `json:"deviceId"`
	ServerVersion string   `json:"serverVersion"`
	Error         string   `json:"error"`
	Paired        bool     `json:"paired"`
	// WaitingSince marks when the pending-approval wait started, so the UI can show
	// how long it has been sitting there.
	WaitingSinceMs int64 `json:"waitingSinceMs,omitempty"`
}

// Event is one gateway event frame, forwarded verbatim to the frontend.
type Event struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// Conn is the live gateway connection. Safe for concurrent use.
type Conn struct {
	mu     sync.RWMutex
	client *ocgateway.Client
	status Status

	identityRoot string

	// cancelPending stops an in-flight approval wait.
	cancelPending context.CancelFunc

	emitEvent  func(Event)
	emitStatus func(Status)
}

func New(identityRoot string, emitEvent func(Event), emitStatus func(Status)) (*Conn, error) {
	return &Conn{
		identityRoot: identityRoot,
		status:       Status{Phase: PhaseIdle},
		emitEvent:    emitEvent,
		emitStatus:   emitStatus,
	}, nil
}

func (c *Conn) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

func (c *Conn) setStatus(mutate func(*Status)) {
	c.mu.Lock()
	mutate(&c.status)
	snapshot := c.status
	c.mu.Unlock()
	if c.emitStatus != nil {
		c.emitStatus(snapshot)
	}
}

// identityStore returns the per-gateway device identity store. Each gateway gets its
// own keypair and device token, so removing one never disturbs the others.
func (c *Conn) identityStore(gatewayID string) (*identity.Store, error) {
	if gatewayID == "" {
		gatewayID = "default"
	}
	dir := filepath.Join(c.identityRoot, gatewayID)
	migrateLegacyIdentity(c.identityRoot, dir)
	return identity.NewStore(dir)
}

// migrateLegacyIdentity moves the pre-multi-gateway identity, which lived directly in
// the root, into the "default" gateway's folder. Without this an upgrade silently
// orphans the existing pairing and asks the user to pair again.
func migrateLegacyIdentity(root, dir string) {
	if filepath.Base(dir) != "default" {
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "keypair.json")); err == nil {
		return // already migrated
	}
	if _, err := os.Stat(filepath.Join(root, "keypair.json")); err != nil {
		return // nothing to migrate
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	for _, name := range []string{"keypair.json", "device-token"} {
		src := filepath.Join(root, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		// Best-effort: a failed move just means the user re-pairs.
		_ = os.Rename(src, filepath.Join(dir, name))
	}
}

// HasStoredPairing reports whether a device token exists for this gateway.
func (c *Conn) HasStoredPairing(gatewayID string) bool {
	store, err := c.identityStore(gatewayID)
	if err != nil {
		return false
	}
	return strings.TrimSpace(store.LoadDeviceToken()) != ""
}

// SetupCode is the decoded payload of an `openclaw qr` setup code.
type SetupCode struct {
	URL            string `json:"url"`
	BootstrapToken string `json:"bootstrapToken"`
	ExpiresAtMs    int64  `json:"expiresAtMs"`
}

// DecodeSetupCode parses a setup code into its gateway URL and one-time token.
func DecodeSetupCode(code string) (SetupCode, error) {
	trimmed := strings.TrimSpace(code)
	trimmed = strings.TrimPrefix(trimmed, "openclaw://setup/")
	trimmed = strings.TrimPrefix(trimmed, "openclaw://")

	raw, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return SetupCode{}, fmt.Errorf("that does not look like a setup code: %w", err)
	}
	var sc SetupCode
	if err := json.Unmarshal(raw, &sc); err != nil {
		return SetupCode{}, fmt.Errorf("setup code is not valid JSON: %w", err)
	}
	if sc.URL == "" || sc.BootstrapToken == "" {
		return SetupCode{}, fmt.Errorf("setup code is missing a gateway URL or bootstrap token")
	}
	if sc.ExpiresAtMs > 0 && time.Now().UnixMilli() > sc.ExpiresAtMs {
		return SetupCode{}, fmt.Errorf("that setup code has expired — run `openclaw qr` for a fresh one")
	}
	return sc, nil
}

// Credential is how ClawHQ authenticates a connect attempt.
type Credential struct {
	// Token is the gateway's shared auth token. Used once, then replaced by the
	// device token the gateway issues.
	Token string
	// BootstrapToken is the one-time token from a setup code.
	BootstrapToken string
}

// Connect attaches to a gateway. With no credential it reuses the stored device token,
// which is the normal path after the first pairing.
func (c *Conn) Connect(ctx context.Context, gatewayID, url string, cred Credential) (Status, error) {
	c.Disconnect()

	if url == "" {
		url = DefaultURL
	}

	store, err := c.identityStore(gatewayID)
	if err != nil {
		return c.Status(), fmt.Errorf("device identity store: %w", err)
	}
	id, err := store.LoadOrGenerate()
	if err != nil {
		return c.Status(), fmt.Errorf("device identity: %w", err)
	}

	c.setStatus(func(s *Status) {
		s.Phase = PhaseConnecting
		s.GatewayID = gatewayID
		s.URL = url
		s.DeviceID = id.DeviceID
		s.Error = ""
		s.WaitingSinceMs = 0
	})

	opts := []ocgateway.Option{
		ocgateway.WithClientInfo(protocol.ClientInfo{
			// The protocol's client id list is a closed enum; "gateway-client" is the
			// entry third-party apps are expected to use.
			ID:       protocol.ClientIDGateway,
			Version:  "0.2.0",
			Mode:     "ui",
			Platform: platform(),
		}),
		ocgateway.WithRole(protocol.RoleOperator),
		ocgateway.WithScopes(Scopes...),
		ocgateway.WithIdentity(id, ""),
		ocgateway.WithOnEvent(func(ev protocol.Event) {
			if c.emitEvent != nil {
				c.emitEvent(Event{Event: string(ev.EventName), Payload: ev.Payload})
			}
		}),
	}

	// Credential precedence: an explicitly supplied one wins, otherwise fall back to
	// the device token from a previous pairing.
	switch {
	case cred.BootstrapToken != "":
		opts = append(opts, ocgateway.WithBootstrapToken(cred.BootstrapToken))
	case cred.Token != "":
		opts = append(opts, ocgateway.WithToken(cred.Token))
	default:
		deviceToken := strings.TrimSpace(store.LoadDeviceToken())
		if deviceToken == "" {
			err := fmt.Errorf("no saved credential for this gateway — enter its token or a setup code")
			c.setStatus(func(s *Status) { s.Phase = PhaseError; s.Error = err.Error() })
			return c.Status(), err
		}
		opts = append(opts, ocgateway.WithDeviceToken(deviceToken))
	}

	client := ocgateway.NewClient(opts...)
	if err := client.Connect(ctx, url); err != nil {
		if isAwaitingApproval(err) {
			// The gateway knows about this device but an operator has not approved it.
			// Park in "pending" and let the caller start a wait.
			c.setStatus(func(s *Status) {
				s.Phase = PhasePending
				s.Error = err.Error()
				s.WaitingSinceMs = time.Now().UnixMilli()
			})
			return c.Status(), nil
		}
		c.setStatus(func(s *Status) { s.Phase = PhaseError; s.Error = err.Error() })
		return c.Status(), err
	}

	hello := client.Hello()
	c.mu.Lock()
	c.client = client
	c.mu.Unlock()

	var scopes []string
	var serverVersion string
	if hello != nil {
		serverVersion = hello.Server.Version
		if hello.Auth != nil {
			scopes = hello.Auth.Scopes
			// The gateway rotates this token; persisting it on every connect keeps
			// reconnects working across a rotation.
			if hello.Auth.DeviceToken != "" {
				_ = store.SaveDeviceToken(hello.Auth.DeviceToken)
			}
		}
	}

	// Scopes can come back empty when a device is known but not yet approved. Treat
	// that as pending rather than reporting a healthy connection the UI cannot use.
	if len(scopes) == 0 {
		client.Close()
		c.mu.Lock()
		c.client = nil
		c.mu.Unlock()
		c.setStatus(func(s *Status) {
			s.Phase = PhasePending
			s.ServerVersion = serverVersion
			s.Error = "connected, but no scopes were granted yet — this device is awaiting approval"
			s.WaitingSinceMs = time.Now().UnixMilli()
		})
		return c.Status(), nil
	}

	c.setStatus(func(s *Status) {
		s.Phase = PhaseConnected
		s.URL = url
		s.Scopes = scopes
		s.ServerVersion = serverVersion
		s.Paired = true
		s.Error = ""
		s.WaitingSinceMs = 0
	})
	return c.Status(), nil
}

// isAwaitingApproval recognises the gateway's "not approved yet" refusals.
//
// The exact wording is not pinned down by the protocol docs, so this matches on the
// phrases the gateway is known to use and errs toward reporting pending — a wrong
// guess here costs a retry, while missing it would strand the user on a raw error.
func isAwaitingApproval(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"pending",
		"await",
		"not approved",
		"approval required",
		"unapproved",
		"needs approval",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// WaitForApproval retries the connection until the device is approved, the context
// ends, or the deadline passes. It is what turns "pending" into "connected" without
// the user having to click connect again after approving.
func (c *Conn) WaitForApproval(ctx context.Context, gatewayID, url string, cred Credential) {
	c.mu.Lock()
	if c.cancelPending != nil {
		c.cancelPending()
	}
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	c.cancelPending = cancel
	c.mu.Unlock()

	go func() {
		defer cancel()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-waitCtx.Done():
				if c.Status().Phase == PhasePending {
					c.setStatus(func(s *Status) {
						s.Phase = PhaseError
						s.Error = "gave up waiting for approval"
					})
				}
				return
			case <-ticker.C:
				if c.Status().Phase != PhasePending {
					return
				}
				attemptCtx, attemptCancel := context.WithTimeout(waitCtx, 30*time.Second)
				status, _ := c.Connect(attemptCtx, gatewayID, url, cred)
				attemptCancel()
				if status.Phase == PhaseConnected {
					return
				}
			}
		}
	}()
}

// CancelApprovalWait stops an in-flight pending retry loop.
func (c *Conn) CancelApprovalWait() {
	c.mu.Lock()
	cancel := c.cancelPending
	c.cancelPending = nil
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.setStatus(func(s *Status) {
		if s.Phase == PhasePending {
			s.Phase = PhaseIdle
			s.WaitingSinceMs = 0
		}
	})
}

func (c *Conn) Disconnect() {
	c.mu.Lock()
	client := c.client
	c.client = nil
	cancel := c.cancelPending
	c.cancelPending = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if client != nil {
		_ = client.Close()
	}
	c.setStatus(func(s *Status) {
		if s.Phase != PhaseIdle {
			s.Phase = PhaseIdle
			s.Scopes = nil
			s.WaitingSinceMs = 0
		}
	})
}

// ForgetPairing drops this gateway's device identity so the next connect pairs anew.
func (c *Conn) ForgetPairing(gatewayID string) error {
	if c.Status().GatewayID == gatewayID {
		c.Disconnect()
	}
	store, err := c.identityStore(gatewayID)
	if err != nil {
		return err
	}
	return store.Reset()
}

// Request performs a raw gateway RPC. One passthrough beats hand-wrapping 424 methods;
// the gateway enforces scopes on its side regardless.
func (c *Conn) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("not connected to a gateway")
	}
	if params == nil {
		params = map[string]any{}
	}

	resp, err := client.Send(ctx, method, params)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		// Surface the gateway's own message; its validation errors name the exact
		// offending property and are far more useful than a generic failure.
		if resp.Error != nil && resp.Error.Message != "" {
			return nil, fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return nil, fmt.Errorf("%s failed", method)
	}
	return resp.Payload, nil
}

// SendChat posts a message into a session. The reply arrives as `chat` stream events,
// not as a return value.
func (c *Conn) SendChat(ctx context.Context, sessionKey, message string) (json.RawMessage, error) {
	return c.Request(ctx, "chat.send", map[string]any{
		"sessionKey":     sessionKey,
		"message":        message,
		"idempotencyKey": newIdempotencyKey(),
	})
}
