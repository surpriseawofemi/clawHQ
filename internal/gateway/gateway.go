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
	// generation is bumped by every connect and Disconnect, so the watcher for a
	// dropped client can tell a deliberate disconnect from a lost connection.
	generation uint64

	emitEvent  func(Event)
	emitStatus func(Status)
	// listeners are Go-side event subscribers; the frontend gets emitEvent.
	listeners []func(Event)
	// onConnected fires after every successful connect, reconnects included.
	onConnected func(Status)
}

func New(identityRoot string, emitEvent func(Event), emitStatus func(Status)) (*Conn, error) {
	return &Conn{
		identityRoot: identityRoot,
		status:       Status{Phase: PhaseIdle},
		emitEvent:    emitEvent,
		emitStatus:   emitStatus,
	}, nil
}

// SetOnConnected installs the callback run after each successful connect.
func (c *Conn) SetOnConnected(fn func(Status)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onConnected = fn
}

// AddEventListener subscribes Go code to gateway events, alongside the frontend.
func (c *Conn) AddEventListener(fn func(Event)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listeners = append(c.listeners, fn)
}

func (c *Conn) dispatchEvent(ev Event) {
	if c.emitEvent != nil {
		c.emitEvent(ev)
	}
	c.mu.RLock()
	listeners := make([]func(Event), len(c.listeners))
	copy(listeners, c.listeners)
	c.mu.RUnlock()
	for _, fn := range listeners {
		fn(ev)
	}
}

func (c *Conn) currentGeneration() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.generation
}

// watch reconnects with the stored device token when the gateway drops the
// connection. A deliberate Disconnect bumps the generation first, so the watcher
// for that client exits instead.
func (c *Conn) watch(client *ocgateway.Client, gen uint64, gatewayID, url string) {
	<-client.Done()
	if c.currentGeneration() != gen {
		return
	}
	c.mu.Lock()
	if c.client == client {
		c.client = nil
	}
	c.mu.Unlock()
	c.setStatus(func(s *Status) {
		s.Phase = PhaseConnecting
		s.Error = "connection to the gateway dropped, reconnecting"
	})

	delay := 2 * time.Second
	for {
		time.Sleep(delay)
		if c.currentGeneration() != gen {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		status, err := c.connect(ctx, gatewayID, url, Credential{}, true)
		cancel()
		if status.Phase == PhaseConnected {
			return
		}
		if err != nil && isPairingRefused(err) {
			// The device token is dead: reconnecting will never work, so stop and
			// let the UI show the pairing form.
			c.setStatus(func(s *Status) { s.Phase = PhaseError; s.Error = err.Error() })
			return
		}
		// connect bumped the generation; follow it so this loop stays the owner.
		gen = c.currentGeneration()
		c.setStatus(func(s *Status) {
			s.Phase = PhaseConnecting
			if err != nil {
				s.Error = "reconnecting: " + err.Error()
			}
		})
		if delay < 30*time.Second {
			delay *= 2
		}
	}
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
	return c.connect(ctx, gatewayID, url, cred, false)
}

// connect is Connect without the teardown. WaitForApproval calls it directly: going
// through Connect would run Disconnect, which cancels cancelPending — the retry
// loop's own context — so the very first retry used to fail with "operation was
// canceled", flip the phase to error, and the loop exited. From the outside that was
// "waiting for approval" silently dropping back to the login form five seconds in.
//
// retrying keeps the phase at pending instead of bouncing through connecting, which
// the onboarding screen renders as the form flashing back.
func (c *Conn) connect(ctx context.Context, gatewayID, url string, cred Credential, retrying bool) (Status, error) {
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

	waitingSince := c.Status().WaitingSinceMs
	c.setStatus(func(s *Status) {
		if !retrying {
			s.Phase = PhaseConnecting
			s.Error = ""
			s.WaitingSinceMs = 0
			waitingSince = 0
		}
		s.GatewayID = gatewayID
		s.URL = url
		s.DeviceID = id.DeviceID
	})
	if waitingSince == 0 {
		waitingSince = time.Now().UnixMilli()
	}

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
			c.dispatchEvent(Event{Event: string(ev.EventName), Payload: ev.Payload})
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
				s.WaitingSinceMs = waitingSince
			})
			return c.Status(), nil
		}
		c.setStatus(func(s *Status) { s.Phase = PhaseError; s.Error = err.Error() })
		return c.Status(), err
	}

	hello := client.Hello()
	c.mu.Lock()
	c.client = client
	c.generation++
	gen := c.generation
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
			s.WaitingSinceMs = waitingSince
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
	go c.watch(client, gen, gatewayID, url)
	c.mu.RLock()
	onConnected := c.onConnected
	c.mu.RUnlock()
	if onConnected != nil {
		go onConnected(c.Status())
	}
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

// retryInterval is how often WaitForApproval re-tries. A variable so tests can
// shorten it.
var retryInterval = 5 * time.Second

// isPairingRefused recognises a refusal that no amount of waiting will fix: the
// operator rejected the device, or the credential we hold is dead.
func isPairingRefused(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"reject",
		"revoked",
		"already used",
		"expired",
		"invalid",
		"unauthorized",
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
//
// Each retry prefers the device token if the gateway has issued one by now, and
// otherwise re-presents the original credential. Transient failures (the tunnel
// blipped, the gateway restarted) keep the phase at pending; only an explicit
// refusal or the deadline gives up.
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
		ticker := time.NewTicker(retryInterval)
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

				attempt := cred
				if store, err := c.identityStore(gatewayID); err == nil {
					if strings.TrimSpace(store.LoadDeviceToken()) != "" {
						attempt = Credential{}
					}
				}

				attemptCtx, attemptCancel := context.WithTimeout(waitCtx, 30*time.Second)
				status, err := c.connect(attemptCtx, gatewayID, url, attempt, true)
				attemptCancel()

				if status.Phase == PhaseConnected {
					return
				}
				if status.Phase == PhasePending {
					continue
				}
				if err != nil && isPairingRefused(err) {
					msg := err.Error()
					if cred.BootstrapToken != "" && attempt.BootstrapToken != "" {
						msg += " — setup codes are single-use, so once the operator has approved this device connect again with the gateway URL + token, or paste a fresh code"
					}
					c.setStatus(func(s *Status) { s.Phase = PhaseError; s.Error = msg })
					return
				}
				// Anything else is transient: stay pending, note what happened.
				c.setStatus(func(s *Status) {
					s.Phase = PhasePending
					if err != nil {
						s.Error = "still waiting — last attempt: " + err.Error()
					}
				})
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
	c.generation++
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
		// offending property and are far more useful than a generic failure. The
		// details ride along for callers that need them (a consent review token, say).
		if resp.Error != nil && resp.Error.Message != "" {
			rpcErr := &RPCError{Method: method, Code: resp.Error.Code, Message: resp.Error.Message}
			if resp.Error.Details != nil {
				if raw, err := json.Marshal(resp.Error.Details); err == nil {
					rpcErr.Details = raw
				}
			}
			return nil, rpcErr
		}
		return nil, fmt.Errorf("%s failed", method)
	}
	return resp.Payload, nil
}

// RPCError is a gateway's refusal of a request, with whatever details it attached.
type RPCError struct {
	Method  string
	Code    string
	Message string
	Details json.RawMessage
}

func (e *RPCError) Error() string { return e.Method + ": " + e.Message }

// DetailString finds a string value by key anywhere inside the error details.
func (e *RPCError) DetailString(key string) string {
	if e == nil || len(e.Details) == 0 {
		return ""
	}
	var walk func(v any) string
	walk = func(v any) string {
		switch t := v.(type) {
		case map[string]any:
			if s, ok := t[key].(string); ok && s != "" {
				return s
			}
			for _, child := range t {
				if s := walk(child); s != "" {
					return s
				}
			}
		case []any:
			for _, child := range t {
				if s := walk(child); s != "" {
					return s
				}
			}
		}
		return ""
	}
	var v any
	if err := json.Unmarshal(e.Details, &v); err != nil {
		return ""
	}
	return walk(v)
}

// SendChat posts a message into a session. The reply arrives as `chat` stream events,
// not as a return value.
func (c *Conn) SendChat(ctx context.Context, sessionKey, message string) (json.RawMessage, error) {
	return c.SendChatWith(ctx, sessionKey, message, nil)
}

// SendChatWith is SendChat plus an attachments array, passed through verbatim.
func (c *Conn) SendChatWith(ctx context.Context, sessionKey, message string, attachments json.RawMessage) (json.RawMessage, error) {
	params := map[string]any{
		"sessionKey":     sessionKey,
		"message":        message,
		"idempotencyKey": newIdempotencyKey(),
	}
	if len(attachments) > 0 {
		params["attachments"] = attachments
	}
	return c.Request(ctx, "chat.send", params)
}
