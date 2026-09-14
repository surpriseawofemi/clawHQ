package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Handler processes ACP agent methods. Implement this interface to handle
// requests from the IDE client.
type Handler interface {
	// Initialize handles the "initialize" request.
	Initialize(ctx context.Context, req InitializeRequest) (*InitializeResponse, error)

	// Authenticate handles the "authenticate" request.
	Authenticate(ctx context.Context, req AuthenticateRequest) (*AuthenticateResponse, error)

	// NewSession handles "session/new".
	NewSession(ctx context.Context, req NewSessionRequest) (*NewSessionResponse, error)

	// LoadSession handles "session/load".
	LoadSession(ctx context.Context, req LoadSessionRequest) (*LoadSessionResponse, error)

	// ListSessions handles "session/list" (UNSTABLE).
	ListSessions(ctx context.Context, req ListSessionsRequest) (*ListSessionsResponse, error)

	// ForkSession handles "session/fork" (UNSTABLE).
	ForkSession(ctx context.Context, req ForkSessionRequest) (*ForkSessionResponse, error)

	// ResumeSession handles "session/resume" (UNSTABLE).
	ResumeSession(ctx context.Context, req ResumeSessionRequest) (*ResumeSessionResponse, error)

	// Prompt handles "session/prompt". This may block for the duration of the turn.
	Prompt(ctx context.Context, req PromptRequest) (*PromptResponse, error)

	// Cancel handles "session/cancel" (notification, no response).
	Cancel(ctx context.Context, req CancelNotification)

	// SetSessionMode handles "session/set_mode".
	SetSessionMode(ctx context.Context, req SetSessionModeRequest) (*SetSessionModeResponse, error)

	// SetSessionModel handles "session/set_model" (UNSTABLE).
	SetSessionModel(ctx context.Context, req SetSessionModelRequest) (*SetSessionModelResponse, error)

	// SetSessionConfigOption handles "session/set_config_option".
	SetSessionConfigOption(ctx context.Context, req SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error)
}

// rpcMessage is a superset of RPCRequest and RPCResponse used for initial
// parsing to distinguish requests from responses on the wire.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// pendingResponse tracks a pending agent→client request awaiting a response.
type pendingResponse struct {
	ch chan RPCResponse
}

// Server is an ACP agent server that reads JSON-RPC 2.0 requests from
// an io.Reader (stdin) and writes responses to an io.Writer (stdout).
type Server struct {
	handler Handler
	reader  io.Reader
	writer  io.Writer
	writeMu sync.Mutex
	seq     atomic.Int64
	done    chan struct{}

	// pending tracks agent→client requests awaiting responses.
	pendingMu sync.Mutex
	pending   map[string]*pendingResponse
}

// NewServer creates an ACP server. The reader/writer should typically be
// os.Stdin and os.Stdout.
func NewServer(handler Handler, reader io.Reader, writer io.Writer) *Server {
	return &Server{
		handler: handler,
		reader:  reader,
		writer:  writer,
		done:    make(chan struct{}),
		pending: make(map[string]*pendingResponse),
	}
}

// Serve reads and processes ACP requests until the reader is closed or
// the context is cancelled. This blocks until finished.
func (s *Server) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.reader)
	// Allow up to 10MB lines for large prompts.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-s.done:
			return nil
		default:
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse as a generic message to distinguish requests from responses.
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			s.sendError(nil, ErrCodeParseError, "parse error")
			continue
		}

		// If there's an ID but no method, this is a response to a pending request.
		if msg.ID != nil && msg.Method == "" {
			s.handleResponse(msg)
			continue
		}

		req := RPCRequest{
			JSONRPC: msg.JSONRPC,
			ID:      msg.ID,
			Method:  msg.Method,
			Params:  msg.Params,
		}
		s.dispatch(ctx, req)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner: %w", err)
	}
	return nil
}

// handleResponse routes an incoming JSON-RPC response to the pending request.
func (s *Server) handleResponse(msg rpcMessage) {
	id := fmt.Sprintf("%v", msg.ID)

	s.pendingMu.Lock()
	pr, ok := s.pending[id]
	if ok {
		delete(s.pending, id)
	}
	s.pendingMu.Unlock()

	if ok {
		resp := RPCResponse{
			JSONRPC: msg.JSONRPC,
			ID:      msg.ID,
			Result:  msg.Result,
			Error:   msg.Error,
		}
		pr.ch <- resp
	}
}

// Close stops the server.
func (s *Server) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// SendNotification sends a notification to the client (no response expected).
func (s *Server) SendNotification(method string, params any) error {
	msg := RPCRequest{
		JSONRPC: "2.0",
		Method:  method,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = data
	}
	return s.writeMessage(msg)
}

// SendRequest sends a request to the client and waits for the response.
// This is used for agent→client requests like fs/read_text_file.
// The caller must ensure Serve() is running to read the response.
func (s *Server) SendRequest(ctx context.Context, method string, params any) (*RPCResponse, error) {
	id := fmt.Sprintf("agent-%d", s.seq.Add(1))
	msg := RPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		msg.Params = data
	}

	// Register pending response before sending.
	pr := &pendingResponse{ch: make(chan RPCResponse, 1)}
	s.pendingMu.Lock()
	s.pending[id] = pr
	s.pendingMu.Unlock()

	if err := s.writeMessage(msg); err != nil {
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, err
	}

	// Wait for response or context cancellation.
	select {
	case resp := <-pr.ch:
		return &resp, nil
	case <-ctx.Done():
		s.pendingMu.Lock()
		delete(s.pending, id)
		s.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// SessionUpdate sends a session/update notification to the client.
func (s *Server) SessionUpdate(notification SessionNotification) error {
	return s.SendNotification("session/update", notification)
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (s *Server) dispatch(ctx context.Context, req RPCRequest) {
	switch req.Method {
	case "initialize":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[InitializeRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.Initialize(ctx, p)
		})
	case "authenticate":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[AuthenticateRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.Authenticate(ctx, p)
		})
	case "session/new":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[NewSessionRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.NewSession(ctx, p)
		})
	case "session/load":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[LoadSessionRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.LoadSession(ctx, p)
		})
	case "session/list":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[ListSessionsRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.ListSessions(ctx, p)
		})
	case "session/fork":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[ForkSessionRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.ForkSession(ctx, p)
		})
	case "session/resume":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[ResumeSessionRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.ResumeSession(ctx, p)
		})
	case "session/prompt":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[PromptRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.Prompt(ctx, p)
		})
	case "session/cancel":
		var params CancelNotification
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &params) // params are optional; zero value is valid fallback
		}
		s.handler.Cancel(ctx, params)
		// No response for notifications.
	case "session/set_mode":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[SetSessionModeRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.SetSessionMode(ctx, p)
		})
	case "session/set_model":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[SetSessionModelRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.SetSessionModel(ctx, p)
		})
	case "session/set_config_option":
		s.handleJSON(ctx, req, func(ctx context.Context, raw json.RawMessage) (any, error) {
			p, err := unmarshalParams[SetSessionConfigOptionRequest](raw)
			if err != nil {
				return nil, err
			}
			return s.handler.SetSessionConfigOption(ctx, p)
		})
	case "$/cancel_request":
		// Protocol-level cancellation notification — no response.
		// For now, we acknowledge but don't propagate to handler.
	default:
		if req.ID != nil {
			s.sendError(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
		}
	}
}

func (s *Server) handleJSON(ctx context.Context, req RPCRequest, fn func(context.Context, json.RawMessage) (any, error)) {
	result, err := fn(ctx, req.Params)
	if req.ID == nil {
		return // notification, no response
	}
	if err != nil {
		s.sendError(req.ID, ErrCodeInternal, err.Error())
		return
	}
	s.sendResult(req.ID, result)
}

func unmarshalParams[T any](params json.RawMessage) (T, error) {
	var v T
	if params != nil {
		if err := json.Unmarshal(params, &v); err != nil {
			return v, fmt.Errorf("invalid params: %w", err)
		}
	}
	return v, nil
}

func (s *Server) sendResult(id any, result any) {
	data, err := json.Marshal(result)
	if err != nil {
		s.sendError(id, ErrCodeInternal, "marshal result: "+err.Error())
		return
	}
	resp := RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	}
	_ = s.writeMessage(resp) // write failure is unrecoverable; read loop detects disconnect
}

func (s *Server) sendError(id any, code int, message string) {
	resp := RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	_ = s.writeMessage(resp) // write failure is unrecoverable; read loop detects disconnect
}

func (s *Server) writeMessage(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.writer.Write(data)
	return err
}
