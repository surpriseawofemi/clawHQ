package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockHandler implements Handler for testing.
type mockHandler struct {
	initResp        *InitializeResponse
	initErr         error
	authResp        *AuthenticateResponse
	authErr         error
	newSessionResp  *NewSessionResponse
	newSessionErr   error
	loadSessionResp *LoadSessionResponse
	loadSessionErr  error
	listResp        *ListSessionsResponse
	listErr         error
	forkResp        *ForkSessionResponse
	forkErr         error
	resumeResp      *ResumeSessionResponse
	resumeErr       error
	promptResp      *PromptResponse
	promptErr       error
	setModeResp     *SetSessionModeResponse
	setModeErr      error
	setModelResp    *SetSessionModelResponse
	setModelErr     error
	setConfigResp   *SetSessionConfigOptionResponse
	setConfigErr    error
	cancelCalled    chan struct{}
}

func (m *mockHandler) Initialize(_ context.Context, _ InitializeRequest) (*InitializeResponse, error) {
	return m.initResp, m.initErr
}

func (m *mockHandler) Authenticate(_ context.Context, _ AuthenticateRequest) (*AuthenticateResponse, error) {
	return m.authResp, m.authErr
}

func (m *mockHandler) NewSession(_ context.Context, _ NewSessionRequest) (*NewSessionResponse, error) {
	return m.newSessionResp, m.newSessionErr
}

func (m *mockHandler) LoadSession(_ context.Context, _ LoadSessionRequest) (*LoadSessionResponse, error) {
	return m.loadSessionResp, m.loadSessionErr
}

func (m *mockHandler) ListSessions(_ context.Context, _ ListSessionsRequest) (*ListSessionsResponse, error) {
	return m.listResp, m.listErr
}

func (m *mockHandler) ForkSession(_ context.Context, _ ForkSessionRequest) (*ForkSessionResponse, error) {
	return m.forkResp, m.forkErr
}

func (m *mockHandler) ResumeSession(_ context.Context, _ ResumeSessionRequest) (*ResumeSessionResponse, error) {
	return m.resumeResp, m.resumeErr
}

func (m *mockHandler) Prompt(_ context.Context, _ PromptRequest) (*PromptResponse, error) {
	return m.promptResp, m.promptErr
}

func (m *mockHandler) Cancel(_ context.Context, _ CancelNotification) {
	if m.cancelCalled != nil {
		close(m.cancelCalled)
	}
}

func (m *mockHandler) SetSessionMode(_ context.Context, _ SetSessionModeRequest) (*SetSessionModeResponse, error) {
	return m.setModeResp, m.setModeErr
}

func (m *mockHandler) SetSessionModel(_ context.Context, _ SetSessionModelRequest) (*SetSessionModelResponse, error) {
	return m.setModelResp, m.setModelErr
}

func (m *mockHandler) SetSessionConfigOption(_ context.Context, _ SetSessionConfigOptionRequest) (*SetSessionConfigOptionResponse, error) {
	return m.setConfigResp, m.setConfigErr
}

// sendAndReadResponse sends a JSON-RPC request and reads the response.
func sendAndReadResponse(t *testing.T, handler *mockHandler, method string, id any, params any) RPCResponse {
	t.Helper()
	req := RPCRequest{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		data, _ := json.Marshal(params)
		req.Params = data
	}
	line, _ := json.Marshal(req)

	input := bytes.NewBuffer(append(line, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Serve(ctx); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	// Give goroutine time to write response.
	time.Sleep(50 * time.Millisecond)

	var resp RPCResponse
	if output.Len() > 0 {
		if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v (raw: %s)", err, output.String())
		}
	}
	return resp
}

// --- Initialize ---

func TestInitialize(t *testing.T) {
	handler := &mockHandler{
		initResp: &InitializeResponse{
			ProtocolVersion: ProtocolVersion,
			AgentInfo:       &Implementation{Name: "test", Version: "1.0"},
		},
	}
	resp := sendAndReadResponse(t, handler, "initialize", "1", InitializeRequest{
		ProtocolVersion:    ProtocolVersion,
		ClientCapabilities: &ClientCapabilities{Terminal: true},
		ClientInfo:         &Implementation{Name: "test-editor", Version: "1.0"},
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	var result InitializeResponse
	json.Unmarshal(resp.Result, &result)
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d", result.ProtocolVersion)
	}
}

func TestInitializeError(t *testing.T) {
	handler := &mockHandler{initErr: fmt.Errorf("init failed")}
	resp := sendAndReadResponse(t, handler, "initialize", "1", InitializeRequest{})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != ErrCodeInternal {
		t.Errorf("code = %d", resp.Error.Code)
	}
}

// --- Authenticate ---

func TestAuthenticate(t *testing.T) {
	handler := &mockHandler{
		authResp: &AuthenticateResponse{},
	}
	resp := sendAndReadResponse(t, handler, "authenticate", "1a", AuthenticateRequest{MethodID: "oauth"})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
}

func TestAuthenticateError(t *testing.T) {
	handler := &mockHandler{authErr: fmt.Errorf("auth failed")}
	resp := sendAndReadResponse(t, handler, "authenticate", "1b", AuthenticateRequest{MethodID: "oauth"})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestAuthenticateInvalidParams(t *testing.T) {
	handler := &mockHandler{authResp: &AuthenticateResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"1c","method":"authenticate","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- NewSession ---

func TestNewSession(t *testing.T) {
	handler := &mockHandler{
		newSessionResp: &NewSessionResponse{SessionID: "sess-1"},
	}
	resp := sendAndReadResponse(t, handler, "session/new", "2", NewSessionRequest{CWD: "/tmp", MCPServers: []MCPServer{}})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	var result NewSessionResponse
	json.Unmarshal(resp.Result, &result)
	if result.SessionID != "sess-1" {
		t.Errorf("sessionId = %q", result.SessionID)
	}
}

// --- LoadSession ---

func TestLoadSession(t *testing.T) {
	handler := &mockHandler{
		loadSessionResp: &LoadSessionResponse{},
	}
	resp := sendAndReadResponse(t, handler, "session/load", "3", LoadSessionRequest{SessionID: "sess-1", CWD: "/tmp", MCPServers: []MCPServer{}})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
}

// --- ListSessions ---

func TestListSessions(t *testing.T) {
	handler := &mockHandler{
		listResp: &ListSessionsResponse{Sessions: []SessionInfo{{SessionID: "s1", CWD: "/tmp"}}},
	}
	resp := sendAndReadResponse(t, handler, "session/list", "4", ListSessionsRequest{})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	var result ListSessionsResponse
	json.Unmarshal(resp.Result, &result)
	if len(result.Sessions) != 1 {
		t.Errorf("sessions = %d", len(result.Sessions))
	}
}

// --- ForkSession ---

func TestForkSession(t *testing.T) {
	handler := &mockHandler{
		forkResp: &ForkSessionResponse{SessionID: "sess-2"},
	}
	resp := sendAndReadResponse(t, handler, "session/fork", "4a", ForkSessionRequest{SessionID: "sess-1", CWD: "/tmp"})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	var result ForkSessionResponse
	json.Unmarshal(resp.Result, &result)
	if result.SessionID != "sess-2" {
		t.Errorf("sessionId = %q", result.SessionID)
	}
}

func TestForkSessionError(t *testing.T) {
	handler := &mockHandler{forkErr: fmt.Errorf("fork failed")}
	resp := sendAndReadResponse(t, handler, "session/fork", "4b", ForkSessionRequest{SessionID: "sess-1", CWD: "/tmp"})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestForkSessionInvalidParams(t *testing.T) {
	handler := &mockHandler{forkResp: &ForkSessionResponse{SessionID: "s2"}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"4c","method":"session/fork","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- ResumeSession ---

func TestResumeSession(t *testing.T) {
	handler := &mockHandler{
		resumeResp: &ResumeSessionResponse{},
	}
	resp := sendAndReadResponse(t, handler, "session/resume", "4d", ResumeSessionRequest{SessionID: "sess-1", CWD: "/tmp"})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
}

func TestResumeSessionError(t *testing.T) {
	handler := &mockHandler{resumeErr: fmt.Errorf("resume failed")}
	resp := sendAndReadResponse(t, handler, "session/resume", "4e", ResumeSessionRequest{SessionID: "sess-1", CWD: "/tmp"})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestResumeSessionInvalidParams(t *testing.T) {
	handler := &mockHandler{resumeResp: &ResumeSessionResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"4f","method":"session/resume","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- Prompt ---

func TestPrompt(t *testing.T) {
	handler := &mockHandler{
		promptResp: &PromptResponse{StopReason: "end_turn"},
	}
	resp := sendAndReadResponse(t, handler, "session/prompt", "5", PromptRequest{
		SessionID: "sess-1",
		Prompt:    []ContentBlock{{Type: "text", Text: "Hello"}},
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	var result PromptResponse
	json.Unmarshal(resp.Result, &result)
	if result.StopReason != "end_turn" {
		t.Errorf("stopReason = %q", result.StopReason)
	}
}

// --- Cancel (notification, no response) ---

func TestCancel(t *testing.T) {
	cancelCalled := make(chan struct{})
	handler := &mockHandler{cancelCalled: cancelCalled}

	// Cancel is a notification (no ID).
	req := RPCRequest{JSONRPC: "2.0", Method: "session/cancel"}
	params, _ := json.Marshal(CancelNotification{SessionID: "sess-1"})
	req.Params = params
	line, _ := json.Marshal(req)

	input := bytes.NewBuffer(append(line, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)

	select {
	case <-cancelCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("cancel not called")
	}

	// No response should be written for a notification.
	time.Sleep(50 * time.Millisecond)
	if output.Len() > 0 {
		t.Errorf("unexpected output for notification: %s", output.String())
	}
}

// --- SetSessionMode ---

func TestSetSessionMode(t *testing.T) {
	handler := &mockHandler{
		setModeResp: &SetSessionModeResponse{},
	}
	resp := sendAndReadResponse(t, handler, "session/set_mode", "6", SetSessionModeRequest{
		SessionID: "sess-1", ModeID: "code",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
}

// --- SetSessionModel ---

func TestSetSessionModel(t *testing.T) {
	handler := &mockHandler{
		setModelResp: &SetSessionModelResponse{},
	}
	resp := sendAndReadResponse(t, handler, "session/set_model", "6a", SetSessionModelRequest{
		SessionID: "sess-1", ModelID: "gpt-4",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
}

func TestSetSessionModelError(t *testing.T) {
	handler := &mockHandler{setModelErr: fmt.Errorf("model not found")}
	resp := sendAndReadResponse(t, handler, "session/set_model", "6b", SetSessionModelRequest{
		SessionID: "sess-1", ModelID: "invalid",
	})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestSetSessionModelInvalidParams(t *testing.T) {
	handler := &mockHandler{setModelResp: &SetSessionModelResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"6c","method":"session/set_model","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- SetSessionConfigOption ---

func TestSetSessionConfigOption(t *testing.T) {
	handler := &mockHandler{
		setConfigResp: &SetSessionConfigOptionResponse{
			ConfigOptions: []SessionConfigOption{
				{Type: "select", ID: "theme", Name: "Theme", CurrentValue: "dark"},
			},
		},
	}
	resp := sendAndReadResponse(t, handler, "session/set_config_option", "6d", SetSessionConfigOptionRequest{
		SessionID: "sess-1", ConfigID: "theme", Value: "dark",
	})
	if resp.Error != nil {
		t.Fatalf("error: %+v", resp.Error)
	}
	var result SetSessionConfigOptionResponse
	json.Unmarshal(resp.Result, &result)
	if len(result.ConfigOptions) != 1 {
		t.Errorf("configOptions = %d", len(result.ConfigOptions))
	}
}

func TestSetSessionConfigOptionError(t *testing.T) {
	handler := &mockHandler{setConfigErr: fmt.Errorf("invalid config")}
	resp := sendAndReadResponse(t, handler, "session/set_config_option", "6e", SetSessionConfigOptionRequest{
		SessionID: "sess-1", ConfigID: "foo", Value: "bar",
	})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestSetSessionConfigOptionInvalidParams(t *testing.T) {
	handler := &mockHandler{setConfigResp: &SetSessionConfigOptionResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"6f","method":"session/set_config_option","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- Unknown method ---

func TestUnknownMethod(t *testing.T) {
	handler := &mockHandler{}
	resp := sendAndReadResponse(t, handler, "unknown/method", "7", nil)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("code = %d", resp.Error.Code)
	}
}

func TestUnknownMethodNotification(t *testing.T) {
	handler := &mockHandler{}

	// Unknown method with no ID (notification) — should not respond.
	req := RPCRequest{JSONRPC: "2.0", Method: "unknown/method"}
	line, _ := json.Marshal(req)

	input := bytes.NewBuffer(append(line, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	if output.Len() > 0 {
		t.Errorf("unexpected output: %s", output.String())
	}
}

// --- $/cancel_request ---

func TestCancelRequest(t *testing.T) {
	handler := &mockHandler{}

	// $/cancel_request is a notification.
	req := RPCRequest{JSONRPC: "2.0", Method: "$/cancel_request"}
	params, _ := json.Marshal(CancelRequestNotification{RequestID: "42"})
	req.Params = params
	line, _ := json.Marshal(req)

	input := bytes.NewBuffer(append(line, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	// No response for $/cancel_request.
	if output.Len() > 0 {
		t.Errorf("unexpected output: %s", output.String())
	}
}

// --- Invalid JSON ---

func TestInvalidJSON(t *testing.T) {
	input := bytes.NewBufferString("not json\n")
	output := &bytes.Buffer{}

	srv := NewServer(&mockHandler{}, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	if output.Len() == 0 {
		t.Fatal("expected error response")
	}
	var resp RPCResponse
	json.Unmarshal(output.Bytes(), &resp)
	if resp.Error == nil || resp.Error.Code != ErrCodeParseError {
		t.Errorf("error = %+v", resp.Error)
	}
}

// --- Invalid params ---

func TestInvalidParams(t *testing.T) {
	handler := &mockHandler{
		initResp: &InitializeResponse{ProtocolVersion: ProtocolVersion},
	}

	req := RPCRequest{JSONRPC: "2.0", ID: "1", Method: "initialize", Params: json.RawMessage(`"not an object"`)}
	line, _ := json.Marshal(req)
	input := bytes.NewBuffer(append(line, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)
	time.Sleep(50 * time.Millisecond)

	var resp RPCResponse
	json.Unmarshal(output.Bytes(), &resp)
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
}

// --- SendNotification ---

func TestSendNotification(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	err := srv.SendNotification("session/update", SessionNotification{
		SessionID: "sess-1",
		Update: SessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       &ContentBlock{Type: "text", Text: "Hello!"},
		},
	})
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	var msg RPCRequest
	if err := json.Unmarshal(output.Bytes(), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Method != "session/update" {
		t.Errorf("method = %q", msg.Method)
	}
	if msg.ID != nil {
		t.Errorf("id should be nil for notifications")
	}
}

func TestSendNotificationNilParams(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	err := srv.SendNotification("test", nil)
	if err != nil {
		t.Fatalf("SendNotification: %v", err)
	}

	var msg RPCRequest
	json.Unmarshal(output.Bytes(), &msg)
	if msg.Params != nil {
		t.Errorf("params should be nil, got %s", msg.Params)
	}
}

// --- SessionUpdate ---

func TestSessionUpdate(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	title := "Run command"
	status := "running"
	err := srv.SessionUpdate(SessionNotification{
		SessionID: "sess-1",
		Update: SessionUpdate{
			SessionUpdate: "tool_call",
			ToolCall: &ToolCall{
				ToolCallID: "tc-1",
				Title:      "Run command",
				Status:     "running",
			},
		},
	})
	_ = title
	_ = status
	if err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("expected output")
	}
}

// --- SendRequest (bidirectional) ---

// safeBuffer is a thread-safe wrapper around bytes.Buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *safeBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func TestSendRequest(t *testing.T) {
	// Create a pipe so the server reads from pr and we can write responses.
	pr, pw := io.Pipe()
	output := &safeBuffer{}

	srv := NewServer(&mockHandler{}, pr, output)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start serving in background.
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- srv.Serve(ctx)
	}()

	// Give Serve time to start scanning.
	time.Sleep(50 * time.Millisecond)

	// Send request in background.
	type sendResult struct {
		resp *RPCResponse
		err  error
	}
	reqDone := make(chan sendResult, 1)
	go func() {
		resp, err := srv.SendRequest(ctx, "fs/read_text_file", ReadTextFileRequest{SessionID: "s1", Path: "/tmp/test.txt"})
		reqDone <- sendResult{resp, err}
	}()

	// Wait for the request to be written.
	time.Sleep(100 * time.Millisecond)

	// Read the outgoing request to get its ID.
	outLines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(outLines) == 0 {
		t.Fatal("no output from SendRequest")
	}
	var outReq RPCRequest
	json.Unmarshal([]byte(outLines[len(outLines)-1]), &outReq)
	if outReq.Method != "fs/read_text_file" {
		t.Fatalf("method = %q", outReq.Method)
	}

	// Send a response back via the pipe.
	respData, _ := json.Marshal(RPCResponse{
		JSONRPC: "2.0",
		ID:      outReq.ID,
		Result:  json.RawMessage(`{"content":"file contents"}`),
	})
	pw.Write(append(respData, '\n'))

	// Wait for SendRequest to complete.
	select {
	case sr := <-reqDone:
		if sr.err != nil {
			t.Fatalf("SendRequest error: %v", sr.err)
		}
		if sr.resp == nil {
			t.Fatal("nil response")
		}
		if sr.resp.Error != nil {
			t.Fatalf("response error: %+v", sr.resp.Error)
		}
		var result ReadTextFileResponse
		json.Unmarshal(sr.resp.Result, &result)
		if result.Content != "file contents" {
			t.Errorf("content = %q", result.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SendRequest timed out")
	}

	// Clean up.
	pw.Close()
	cancel()
	<-serveDone
}

func TestSendRequestContextCancel(t *testing.T) {
	// Test that SendRequest returns when context is cancelled.
	pr, pw := io.Pipe()
	defer pw.Close()
	output := &safeBuffer{}

	srv := NewServer(&mockHandler{}, pr, output)
	serveCtx, serveCancel := context.WithCancel(context.Background())
	defer serveCancel()

	go func() {
		srv.Serve(serveCtx)
	}()

	time.Sleep(50 * time.Millisecond)

	reqCtx, reqCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer reqCancel()

	_, err := srv.SendRequest(reqCtx, "fs/read_text_file", ReadTextFileRequest{SessionID: "s1", Path: "/tmp/test.txt"})
	if err == nil {
		t.Fatal("expected error")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v", err)
	}

	// Clean up.
	pw.Close()
	serveCancel()
}

func TestSendRequestNilParams(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	// Can't wait for response without Serve running, but verify the write works.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := srv.SendRequest(ctx, "test", nil)
	// Will get context deadline since no one sends a response.
	if err != context.DeadlineExceeded {
		t.Errorf("err = %v", err)
	}

	// Verify the message was written.
	if output.Len() == 0 {
		t.Fatal("expected output")
	}
	var msg RPCRequest
	json.Unmarshal(output.Bytes(), &msg)
	if msg.Method != "test" {
		t.Errorf("method = %q", msg.Method)
	}
}

func TestSendRequestMarshalError(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	ctx := context.Background()
	_, err := srv.SendRequest(ctx, "test", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSendRequestWriteError(t *testing.T) {
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, &errorWriter{})

	ctx := context.Background()
	_, err := srv.SendRequest(ctx, "test", nil)
	if err == nil {
		t.Fatal("expected write error")
	}
}

// --- Close ---

func TestClose(t *testing.T) {
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, &bytes.Buffer{})
	srv.Close()
	// Double close should be safe.
	srv.Close()
}

// --- Serve with context cancellation ---

func TestServeContextCancel(t *testing.T) {
	// Create a reader that blocks forever.
	pr, _ := io.Pipe()
	defer pr.Close()

	srv := NewServer(&mockHandler{}, pr, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx)
	}()

	cancel()
	// Close the server to unblock scanner.Scan().
	srv.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after context cancel")
	}
}

func TestServeCloseDone(t *testing.T) {
	// Test the s.done path: close the server before calling Serve.
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, &bytes.Buffer{})
	srv.Close()

	err := srv.Serve(context.Background())
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// --- Empty lines ---

func TestServeEmptyLines(t *testing.T) {
	input := bytes.NewBufferString("\n\n")
	output := &bytes.Buffer{}

	srv := NewServer(&mockHandler{}, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := srv.Serve(ctx)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if output.Len() > 0 {
		t.Errorf("unexpected output: %s", output.String())
	}
}

// --- Multiple requests ---

func TestMultipleRequests(t *testing.T) {
	handler := &mockHandler{
		initResp:       &InitializeResponse{ProtocolVersion: ProtocolVersion},
		newSessionResp: &NewSessionResponse{SessionID: "sess-1"},
	}

	var lines []string
	req1 := RPCRequest{JSONRPC: "2.0", ID: "1", Method: "initialize"}
	data1, _ := json.Marshal(req1)
	lines = append(lines, string(data1))

	req2 := RPCRequest{JSONRPC: "2.0", ID: "2", Method: "session/new"}
	data2, _ := json.Marshal(req2)
	lines = append(lines, string(data2))

	input := bytes.NewBufferString(strings.Join(lines, "\n") + "\n")
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)
	time.Sleep(100 * time.Millisecond)

	// Should have two responses.
	outLines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(outLines) < 2 {
		t.Fatalf("expected 2 responses, got %d: %s", len(outLines), output.String())
	}
}

// --- Response handling (handleResponse) ---

func TestHandleResponseNoMatch(t *testing.T) {
	// Sending a response with no matching pending request should not panic.
	resp := RPCResponse{JSONRPC: "2.0", ID: "nonexistent", Result: json.RawMessage(`{}`)}
	data, _ := json.Marshal(resp)

	input := bytes.NewBuffer(append(data, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(&mockHandler{}, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := srv.Serve(ctx)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

// --- Type round trip tests ---

func TestTypesRoundTrip(t *testing.T) {
	desc := func(s string) *string { return &s }
	num := func(n int) *int { return &n }
	num64 := func(n int64) *int64 { return &n }

	tests := []struct {
		name string
		val  any
	}{
		{"RPCRequest", RPCRequest{JSONRPC: "2.0", ID: "1", Method: "test"}},
		{"RPCResponse", RPCResponse{JSONRPC: "2.0", ID: "1", Result: json.RawMessage(`{}`)}},
		{"RPCError", RPCError{Code: -32600, Message: "invalid"}},
		{"InitializeRequest", InitializeRequest{ProtocolVersion: 1}},
		{"InitializeResponse", InitializeResponse{
			ProtocolVersion: 1,
			AgentInfo:       &Implementation{Name: "test", Version: "1"},
			AuthMethods:     []AuthMethod{{ID: "oauth", Name: "OAuth", Description: desc("OAuth 2.0")}},
		}},
		{"ClientCapabilities", ClientCapabilities{
			FS:       &FileSystemCapability{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		}},
		{"AgentCapabilities", AgentCapabilities{
			LoadSession:        true,
			PromptCapabilities: &PromptCapabilities{Image: true, EmbeddedContext: true},
			MCPCapabilities:    &MCPCapabilities{HTTP: true},
			SessionCapabilities: &SessionCapabilities{
				List:   &SessionListCapabilities{},
				Fork:   &SessionForkCapabilities{},
				Resume: &SessionResumeCapabilities{},
			},
		}},
		{"AuthMethod", AuthMethod{ID: "oauth", Name: "OAuth"}},
		{"AuthenticateRequest", AuthenticateRequest{MethodID: "oauth"}},
		{"AuthenticateResponse", AuthenticateResponse{}},
		{"MCPServer-stdio", MCPServer{Name: "s1", Command: "cmd", Args: []string{"a"}, Env: []EnvVariable{{Name: "K", Value: "V"}}}},
		{"MCPServer-http", MCPServer{Type: "http", Name: "s2", URL: "http://localhost", Headers: []HTTPHeader{{Name: "Auth", Value: "Bearer x"}}}},
		{"NewSessionRequest", NewSessionRequest{CWD: "/tmp", MCPServers: []MCPServer{{Name: "s1", Command: "cmd"}}}},
		{"NewSessionResponse", NewSessionResponse{
			SessionID:     "s1",
			Modes:         &SessionModeState{AvailableModes: []SessionMode{{ID: "code", Name: "Code"}}, CurrentModeID: "code"},
			Models:        &SessionModelState{AvailableModels: []ModelInfo{{ModelID: "gpt-4", Name: "GPT-4"}}, CurrentModelID: "gpt-4"},
			ConfigOptions: []SessionConfigOption{{Type: "select", ID: "theme", Name: "Theme", CurrentValue: "dark"}},
		}},
		{"LoadSessionRequest", LoadSessionRequest{SessionID: "s1", CWD: "/tmp", MCPServers: []MCPServer{}}},
		{"LoadSessionResponse", LoadSessionResponse{}},
		{"ListSessionsRequest", ListSessionsRequest{Cursor: desc("c1"), CWD: desc("/tmp")}},
		{"ListSessionsResponse", ListSessionsResponse{Sessions: []SessionInfo{{SessionID: "s1", CWD: "/tmp", Title: desc("Test")}}}},
		{"ForkSessionRequest", ForkSessionRequest{SessionID: "s1", CWD: "/tmp"}},
		{"ForkSessionResponse", ForkSessionResponse{SessionID: "s2"}},
		{"ResumeSessionRequest", ResumeSessionRequest{SessionID: "s1", CWD: "/tmp"}},
		{"ResumeSessionResponse", ResumeSessionResponse{}},
		{"SetSessionModeRequest", SetSessionModeRequest{SessionID: "s1", ModeID: "code"}},
		{"SetSessionModelRequest", SetSessionModelRequest{SessionID: "s1", ModelID: "gpt-4"}},
		{"SetSessionConfigOptionRequest", SetSessionConfigOptionRequest{SessionID: "s1", ConfigID: "theme", Value: "dark"}},
		{"SetSessionConfigOptionResponse", SetSessionConfigOptionResponse{ConfigOptions: []SessionConfigOption{{Type: "select", ID: "t", Name: "T"}}}},
		{"PromptRequest", PromptRequest{SessionID: "s1", Prompt: []ContentBlock{{Type: "text", Text: "hi"}}}},
		{"PromptResponse", PromptResponse{StopReason: "end_turn", Usage: &Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}}},
		{"ContentBlock-text", ContentBlock{Type: "text", Text: "hello"}},
		{"ContentBlock-image", ContentBlock{Type: "image", MimeType: "image/png", Data: "base64data", URI: desc("https://example.com/img.png")}},
		{"ContentBlock-audio", ContentBlock{Type: "audio", MimeType: "audio/wav", Data: "base64data"}},
		{"ContentBlock-resource_link", ContentBlock{Type: "resource_link", URI: desc("file://test"), Name: desc("test.go"), Title: desc("Test File"), Size: num64(100)}},
		{"ContentBlock-resource", ContentBlock{Type: "resource", Resource: &EmbeddedResourceContents{URI: "file://test", Text: "code"}}},
		{"EmbeddedResourceContents-text", EmbeddedResourceContents{URI: "file://test", Text: "code", MimeType: desc("text/plain")}},
		{"EmbeddedResourceContents-blob", EmbeddedResourceContents{URI: "file://test", Blob: "base64data"}},
		{"CancelNotification", CancelNotification{SessionID: "s1"}},
		{"CancelRequestNotification", CancelRequestNotification{RequestID: "42"}},
		{"Annotations", Annotations{Audience: []Role{"user"}, Priority: func() *float64 { v := 1.0; return &v }()}},
		// ToolCall types.
		{"ToolCall", ToolCall{ToolCallID: "tc1", Title: "Run", Kind: ToolKindExecute, Status: ToolCallStatusRunning}},
		{"ToolCallUpdate", ToolCallUpdate{ToolCallID: "tc1", Status: desc(ToolCallStatusCompleted)}},
		{"ToolCallContent-content", ToolCallContent{Type: "content", Content: &ContentBlock{Type: "text", Text: "output"}}},
		{"ToolCallContent-diff", ToolCallContent{Type: "diff", Path: "/tmp/file", OldText: desc("old"), NewText: "new"}},
		{"ToolCallContent-terminal", ToolCallContent{Type: "terminal", TerminalID: "t1"}},
		{"ToolCallLocation", ToolCallLocation{Path: "/tmp/file", Line: num(42)}},
		// Plan types.
		{"Plan", Plan{Entries: []PlanEntry{{Content: "do thing", Priority: PlanEntryPriorityHigh, Status: PlanEntryStatusPending}}}},
		// Mode types.
		{"SessionMode", SessionMode{ID: "code", Name: "Code", Description: desc("Write code")}},
		{"SessionModeState", SessionModeState{AvailableModes: []SessionMode{{ID: "code", Name: "Code"}}, CurrentModeID: "code"}},
		{"CurrentModeUpdate", CurrentModeUpdate{CurrentModeID: "code"}},
		// Model types.
		{"ModelInfo", ModelInfo{ModelID: "gpt-4", Name: "GPT-4"}},
		{"SessionModelState", SessionModelState{AvailableModels: []ModelInfo{{ModelID: "gpt-4", Name: "GPT-4"}}, CurrentModelID: "gpt-4"}},
		// Config option types.
		{"SessionConfigOption", SessionConfigOption{Type: "select", ID: "theme", Name: "Theme", Category: desc("mode"), CurrentValue: "dark", Options: json.RawMessage(`[]`)}},
		{"SessionConfigSelectOption", SessionConfigSelectOption{Value: "dark", Name: "Dark"}},
		{"SessionConfigSelectGroup", SessionConfigSelectGroup{Group: "g1", Name: "Group 1", Options: []SessionConfigSelectOption{{Value: "dark", Name: "Dark"}}}},
		{"ConfigOptionUpdate", ConfigOptionUpdate{ConfigOptions: []SessionConfigOption{{Type: "select", ID: "theme", Name: "Theme"}}}},
		// Session info types.
		{"SessionInfo", SessionInfo{SessionID: "s1", CWD: "/tmp", Title: desc("Test"), UpdatedAt: desc("2025-01-01T00:00:00Z")}},
		{"SessionInfoUpdate", SessionInfoUpdate{Title: desc("Updated")}},
		// Usage types.
		{"Usage", Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30, CachedReadTokens: num(5), ThoughtTokens: num(2)}},
		{"UsageUpdate", UsageUpdate{Size: 100, Used: 50, Cost: &Cost{Amount: 0.01, Currency: "USD"}}},
		{"Cost", Cost{Amount: 0.01, Currency: "USD"}},
		// Command types.
		{"AvailableCommand", AvailableCommand{Name: "/help", Description: "Show help", Input: &AvailableCommandInput{Hint: "query"}}},
		{"AvailableCommandsUpdate", AvailableCommandsUpdate{AvailableCommands: []AvailableCommand{{Name: "/help", Description: "Help"}}}},
		// Permission types.
		{"PermissionOption", PermissionOption{OptionID: "opt1", Name: "Allow once", Kind: PermissionOptionKindAllowOnce}},
		{"RequestPermissionOutcome-cancelled", RequestPermissionOutcome{Outcome: "cancelled"}},
		{"RequestPermissionOutcome-selected", RequestPermissionOutcome{Outcome: "selected", OptionID: "opt1"}},
		// Session update variants.
		{"SessionUpdate-agent_message_chunk", SessionUpdate{SessionUpdate: "agent_message_chunk", Content: &ContentBlock{Type: "text", Text: "hi"}}},
		{"SessionUpdate-tool_call", SessionUpdate{SessionUpdate: "tool_call", ToolCall: &ToolCall{ToolCallID: "tc1", Title: "Run"}}},
		{"SessionUpdate-tool_call_update", SessionUpdate{SessionUpdate: "tool_call_update", ToolCallUpdate: &ToolCallUpdate{ToolCallID: "tc1"}}},
		{"SessionUpdate-plan", SessionUpdate{SessionUpdate: "plan", Plan: &Plan{Entries: []PlanEntry{{Content: "do", Priority: "high", Status: "pending"}}}}},
		{"SessionUpdate-available_commands_update", SessionUpdate{SessionUpdate: "available_commands_update", AvailableCommands: []AvailableCommand{{Name: "/help", Description: "Help"}}}},
		{"SessionUpdate-current_mode_update", SessionUpdate{SessionUpdate: "current_mode_update", CurrentModeID: desc("code")}},
		{"SessionUpdate-config_option_update", SessionUpdate{SessionUpdate: "config_option_update", ConfigOptions: []SessionConfigOption{{Type: "select", ID: "t", Name: "T"}}}},
		{"SessionUpdate-session_info_update", SessionUpdate{SessionUpdate: "session_info_update", Title: desc("Updated")}},
		{"SessionUpdate-usage_update", SessionUpdate{SessionUpdate: "usage_update", Size: num(100), Used: num(50)}},
		{"SessionNotification", SessionNotification{SessionID: "s1", Update: SessionUpdate{SessionUpdate: "agent_message_chunk", Content: &ContentBlock{Type: "text", Text: "hi"}}}},
		// Agent→Client request types.
		{"RequestPermissionRequest", RequestPermissionRequest{
			SessionID: "s1",
			ToolCall:  ToolCallUpdate{ToolCallID: "tc1"},
			Options:   []PermissionOption{{OptionID: "opt1", Name: "Allow", Kind: "allow_once"}},
		}},
		{"RequestPermissionResponse", RequestPermissionResponse{Outcome: RequestPermissionOutcome{Outcome: "selected", OptionID: "opt1"}}},
		{"ReadTextFileRequest", ReadTextFileRequest{SessionID: "s1", Path: "/tmp/test", Line: num(10), Limit: num(100)}},
		{"ReadTextFileResponse", ReadTextFileResponse{Content: "content"}},
		{"WriteTextFileRequest", WriteTextFileRequest{SessionID: "s1", Path: "/tmp/test", Content: "data"}},
		{"WriteTextFileResponse", WriteTextFileResponse{}},
		{"CreateTerminalRequest", CreateTerminalRequest{
			SessionID:       "s1",
			Command:         "ls",
			Args:            []string{"-la"},
			CWD:             desc("/tmp"),
			Env:             []EnvVariable{{Name: "PATH", Value: "/usr/bin"}},
			OutputByteLimit: num64(1024),
		}},
		{"CreateTerminalResponse", CreateTerminalResponse{TerminalID: "t1"}},
		{"TerminalOutputRequest", TerminalOutputRequest{SessionID: "s1", TerminalID: "t1"}},
		{"TerminalOutputResponse", TerminalOutputResponse{
			Output:     "files",
			Truncated:  true,
			ExitStatus: &TerminalExitStatus{ExitCode: num(0)},
		}},
		{"TerminalReleaseRequest", TerminalReleaseRequest{SessionID: "s1", TerminalID: "t1"}},
		{"TerminalReleaseResponse", TerminalReleaseResponse{}},
		{"TerminalWaitForExitRequest", TerminalWaitForExitRequest{SessionID: "s1", TerminalID: "t1"}},
		{"TerminalWaitForExitResponse", TerminalWaitForExitResponse{ExitCode: num(0), Signal: desc("SIGTERM")}},
		{"TerminalKillRequest", TerminalKillRequest{SessionID: "s1", TerminalID: "t1"}},
		{"TerminalKillResponse", TerminalKillResponse{}},
		{"TerminalExitStatus", TerminalExitStatus{ExitCode: num(0), Signal: desc("SIGTERM")}},
		{"ContentWrapper", ContentWrapper{Content: ContentBlock{Type: "text", Text: "hi"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if len(data) == 0 {
				t.Error("empty JSON")
			}
		})
	}
}

// ToolCallStatusRunning is an alias for in_progress used in tests.
const ToolCallStatusRunning = "in_progress"

// --- Unmarshal error paths for all methods ---

func sendRawAndReadResponse(t *testing.T, handler *mockHandler, rawJSON string) RPCResponse {
	t.Helper()
	input := bytes.NewBufferString(rawJSON + "\n")
	output := &bytes.Buffer{}
	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	srv.Serve(ctx)
	var resp RPCResponse
	if output.Len() > 0 {
		json.Unmarshal(output.Bytes(), &resp)
	}
	return resp
}

func TestNewSessionInvalidParams(t *testing.T) {
	handler := &mockHandler{newSessionResp: &NewSessionResponse{SessionID: "s1"}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"10","method":"session/new","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestLoadSessionInvalidParams(t *testing.T) {
	handler := &mockHandler{loadSessionResp: &LoadSessionResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"11","method":"session/load","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestListSessionsInvalidParams(t *testing.T) {
	handler := &mockHandler{listResp: &ListSessionsResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"12","method":"session/list","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestPromptInvalidParams(t *testing.T) {
	handler := &mockHandler{promptResp: &PromptResponse{StopReason: "end_turn"}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"13","method":"session/prompt","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestSetSessionModeInvalidParams(t *testing.T) {
	handler := &mockHandler{setModeResp: &SetSessionModeResponse{}}
	resp := sendRawAndReadResponse(t, handler, `{"jsonrpc":"2.0","id":"14","method":"session/set_mode","params":"bad"}`)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

// --- handleJSON notification path (ID is nil) ---

func TestHandleJSONNotificationNoResponse(t *testing.T) {
	handler := &mockHandler{
		initResp: &InitializeResponse{ProtocolVersion: ProtocolVersion},
	}

	// Send initialize as a notification (no ID) - handler runs but no response.
	req := RPCRequest{JSONRPC: "2.0", Method: "initialize"}
	params, _ := json.Marshal(InitializeRequest{ProtocolVersion: 1})
	req.Params = params
	line, _ := json.Marshal(req)

	input := bytes.NewBuffer(append(line, '\n'))
	output := &bytes.Buffer{}

	srv := NewServer(handler, input, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	srv.Serve(ctx)

	// Should not write any response for notification.
	if output.Len() > 0 {
		t.Errorf("unexpected output for notification: %s", output.String())
	}
}

// --- sendResult marshal error ---

func TestSendResultMarshalError(t *testing.T) {
	req := RPCRequest{JSONRPC: "2.0", ID: "20", Method: "initialize"}
	line, _ := json.Marshal(req)

	input := bytes.NewBuffer(append(line, '\n'))

	srv := NewServer(&mockHandler{initResp: &InitializeResponse{ProtocolVersion: 1}}, input, &errorWriter{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should not panic even if write fails.
	srv.Serve(ctx)
}

type errorWriter struct{}

func (e *errorWriter) Write(p []byte) (int, error) {
	return 0, fmt.Errorf("write error")
}

// --- Scanner error path ---

func TestServeScannerError(t *testing.T) {
	r := &errorReader{}
	output := &bytes.Buffer{}

	srv := NewServer(&mockHandler{}, r, output)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := srv.Serve(ctx)
	if err == nil || !strings.Contains(err.Error(), "scanner") {
		t.Errorf("expected scanner error, got: %v", err)
	}
}

type errorReader struct {
	called bool
}

func (e *errorReader) Read(p []byte) (int, error) {
	if !e.called {
		e.called = true
		copy(p, []byte("partial"))
		return 7, fmt.Errorf("read error")
	}
	return 0, io.EOF
}

// --- SendNotification marshal error ---

func TestSendNotificationMarshalError(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	// Channel can't be marshaled.
	err := srv.SendNotification("test", make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

// --- Serve with already-cancelled context ---

func TestServeAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, &bytes.Buffer{})
	err := srv.Serve(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- sendResult marshal error (direct call) ---

func TestSendResultMarshalErrorDirect(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	// Passing a channel which json.Marshal cannot handle.
	srv.sendResult("99", make(chan int))

	var resp RPCResponse
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw: %s)", err, output.String())
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != ErrCodeInternal {
		t.Errorf("code = %d, want %d", resp.Error.Code, ErrCodeInternal)
	}
}

// --- writeMessage marshal error (direct call) ---

func TestWriteMessageMarshalErrorDirect(t *testing.T) {
	output := &bytes.Buffer{}
	srv := NewServer(&mockHandler{}, &bytes.Buffer{}, output)

	err := srv.writeMessage(make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if output.Len() != 0 {
		t.Errorf("unexpected output: %s", output.String())
	}
}

// --- Constants ---

func TestConstants(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d", ProtocolVersion)
	}
	if ErrCodeParseError != -32700 {
		t.Errorf("ErrCodeParseError = %d", ErrCodeParseError)
	}
	if ErrCodeInvalidRequest != -32600 {
		t.Errorf("ErrCodeInvalidRequest = %d", ErrCodeInvalidRequest)
	}
	if ErrCodeMethodNotFound != -32601 {
		t.Errorf("ErrCodeMethodNotFound = %d", ErrCodeMethodNotFound)
	}
	if ErrCodeInvalidParams != -32602 {
		t.Errorf("ErrCodeInvalidParams = %d", ErrCodeInvalidParams)
	}
	if ErrCodeInternal != -32603 {
		t.Errorf("ErrCodeInternal = %d", ErrCodeInternal)
	}
	if ErrCodeRequestCancelled != -32800 {
		t.Errorf("ErrCodeRequestCancelled = %d", ErrCodeRequestCancelled)
	}
	if ErrCodeAuthenticationNeeded != -32000 {
		t.Errorf("ErrCodeAuthenticationNeeded = %d", ErrCodeAuthenticationNeeded)
	}
	if ErrCodeResourceNotFound != -32002 {
		t.Errorf("ErrCodeResourceNotFound = %d", ErrCodeResourceNotFound)
	}
}

// --- String constants ---

func TestStringConstants(t *testing.T) {
	// StopReason
	if StopReasonEndTurn != "end_turn" {
		t.Error("StopReasonEndTurn")
	}
	if StopReasonMaxTokens != "max_tokens" {
		t.Error("StopReasonMaxTokens")
	}
	if StopReasonMaxTurnRequests != "max_turn_requests" {
		t.Error("StopReasonMaxTurnRequests")
	}
	if StopReasonRefusal != "refusal" {
		t.Error("StopReasonRefusal")
	}
	if StopReasonCancelled != "cancelled" {
		t.Error("StopReasonCancelled")
	}

	// ToolCallStatus
	if ToolCallStatusPending != "pending" {
		t.Error("ToolCallStatusPending")
	}
	if ToolCallStatusInProgress != "in_progress" {
		t.Error("ToolCallStatusInProgress")
	}
	if ToolCallStatusCompleted != "completed" {
		t.Error("ToolCallStatusCompleted")
	}
	if ToolCallStatusFailed != "failed" {
		t.Error("ToolCallStatusFailed")
	}

	// ToolKind
	if ToolKindRead != "read" {
		t.Error("ToolKindRead")
	}
	if ToolKindEdit != "edit" {
		t.Error("ToolKindEdit")
	}
	if ToolKindDelete != "delete" {
		t.Error("ToolKindDelete")
	}
	if ToolKindMove != "move" {
		t.Error("ToolKindMove")
	}
	if ToolKindSearch != "search" {
		t.Error("ToolKindSearch")
	}
	if ToolKindExecute != "execute" {
		t.Error("ToolKindExecute")
	}
	if ToolKindThink != "think" {
		t.Error("ToolKindThink")
	}
	if ToolKindFetch != "fetch" {
		t.Error("ToolKindFetch")
	}
	if ToolKindSwitchMode != "switch_mode" {
		t.Error("ToolKindSwitchMode")
	}
	if ToolKindOther != "other" {
		t.Error("ToolKindOther")
	}

	// PermissionOptionKind
	if PermissionOptionKindAllowOnce != "allow_once" {
		t.Error("PermissionOptionKindAllowOnce")
	}
	if PermissionOptionKindAllowAlways != "allow_always" {
		t.Error("PermissionOptionKindAllowAlways")
	}
	if PermissionOptionKindRejectOnce != "reject_once" {
		t.Error("PermissionOptionKindRejectOnce")
	}
	if PermissionOptionKindRejectAlways != "reject_always" {
		t.Error("PermissionOptionKindRejectAlways")
	}

	// PlanEntryPriority
	if PlanEntryPriorityHigh != "high" {
		t.Error("PlanEntryPriorityHigh")
	}
	if PlanEntryPriorityMedium != "medium" {
		t.Error("PlanEntryPriorityMedium")
	}
	if PlanEntryPriorityLow != "low" {
		t.Error("PlanEntryPriorityLow")
	}

	// PlanEntryStatus
	if PlanEntryStatusPending != "pending" {
		t.Error("PlanEntryStatusPending")
	}
	if PlanEntryStatusInProgress != "in_progress" {
		t.Error("PlanEntryStatusInProgress")
	}
	if PlanEntryStatusCompleted != "completed" {
		t.Error("PlanEntryStatusCompleted")
	}

	// SessionConfigOptionCategory
	if SessionConfigCategoryMode != "mode" {
		t.Error("SessionConfigCategoryMode")
	}
	if SessionConfigCategoryModel != "model" {
		t.Error("SessionConfigCategoryModel")
	}
	if SessionConfigCategoryThoughtLevel != "thought_level" {
		t.Error("SessionConfigCategoryThoughtLevel")
	}
}
