package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			t.Errorf("auth = %q, want 'Bearer test-tok'", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-openclaw-agent-id") != "main" {
			t.Errorf("agent-id = %q, want 'main'", r.Header.Get("x-openclaw-agent-id"))
		}

		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Model != "openclaw:main" {
			t.Errorf("model = %q, want 'openclaw:main'", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Content != "hi" {
			t.Errorf("messages = %+v", req.Messages)
		}

		resp := Response{
			ID:      "chatcmpl-1",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "openclaw:main",
			Choices: []Choice{{
				Index:        0,
				Message:      Message{Role: "assistant", Content: "Hello!"},
				FinishReason: "stop",
			}},
			Usage: &Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{
		BaseURL: srv.URL,
		Token:   "test-tok",
		AgentID: "main",
	}

	resp, err := client.Create(context.Background(), Request{
		Model:    "openclaw:main",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID != "chatcmpl-1" {
		t.Errorf("id = %q, want 'chatcmpl-1'", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices len = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "Hello!" {
		t.Errorf("content = %q, want 'Hello!'", resp.Choices[0].Message.Content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 8 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestCreateWithCustomHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{ID: "1", Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{
		BaseURL:    srv.URL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
	resp, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID != "1" {
		t.Errorf("id = %q", resp.ID)
	}
}

func TestCreateNoTokenNoAgentNoSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("auth should be empty, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-openclaw-agent-id") != "" {
			t.Errorf("agent-id should be empty, got %q", r.Header.Get("x-openclaw-agent-id"))
		}
		if r.Header.Get("x-openclaw-session-key") != "" {
			t.Errorf("session-key should be empty, got %q", r.Header.Get("x-openclaw-session-key"))
		}
		resp := Response{ID: "1", Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("stream = false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("not a flusher")
		}

		chunks := []StreamChunk{
			{
				ID: "chatcmpl-1", Object: "chat.completion.chunk",
				Choices: []StreamDelta{{Index: 0, Delta: DeltaContent{Role: "assistant"}}},
			},
			{
				ID: "chatcmpl-1", Object: "chat.completion.chunk",
				Choices: []StreamDelta{{Index: 0, Delta: DeltaContent{Content: "Hello"}}},
			},
			{
				ID: "chatcmpl-1", Object: "chat.completion.chunk",
				Choices: []StreamDelta{{Index: 0, Delta: DeltaContent{Content: "!"}, FinishReason: strPtr("stop")}},
			},
		}
		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	stream, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw:main",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	var contents []string
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			contents = append(contents, chunk.Choices[0].Delta.Content)
		}
	}
	if len(contents) != 2 {
		t.Fatalf("chunks = %d, want 2", len(contents))
	}
	if contents[0] != "Hello" || contents[1] != "!" {
		t.Errorf("contents = %v, want [Hello !]", contents)
	}
}

func TestCreateStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 401 {
		t.Errorf("status = %d, want 401", httpErr.StatusCode)
	}
}

func TestCreateHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw:main",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 401 {
		t.Errorf("status = %d, want 401", httpErr.StatusCode)
	}
}

func TestHTTPErrorString(t *testing.T) {
	e := &HTTPError{StatusCode: 500, Body: "server error"}
	s := e.Error()
	if !strings.Contains(s, "500") || !strings.Contains(s, "server error") {
		t.Errorf("Error() = %q", s)
	}
}

func TestHTTPErrorRetryAfter(t *testing.T) {
	e := &HTTPError{StatusCode: 429, Body: "rate limited", RetryAfter: "60"}
	s := e.Error()
	if !strings.Contains(s, "429") || !strings.Contains(s, "retry-after: 60") {
		t.Errorf("Error() = %q", s)
	}
}

func TestHTTPErrorNoRetryAfter(t *testing.T) {
	e := &HTTPError{StatusCode: 429, Body: "rate limited"}
	s := e.Error()
	if strings.Contains(s, "retry-after") {
		t.Errorf("Error() = %q, should not contain retry-after", s)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := c.Create(context.Background(), Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if httpErr.RetryAfter != "30" {
		t.Errorf("RetryAfter = %q, want '30'", httpErr.RetryAfter)
	}
}

func TestRetryAfterHeaderStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := c.CreateStream(context.Background(), Request{Model: "test", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected *HTTPError, got %T", err)
	}
	if httpErr.RetryAfter != "45" {
		t.Errorf("RetryAfter = %q, want '45'", httpErr.RetryAfter)
	}
}

func TestSessionKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-openclaw-session-key") != "custom-session" {
			t.Errorf("session key = %q, want 'custom-session'", r.Header.Get("x-openclaw-session-key"))
		}
		resp := Response{ID: "1", Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok", SessionKey: "custom-session"}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error = %q, want to contain 'decode response'", err.Error())
	}
}

func TestCreateDoRequestError(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:1"} // nothing listening
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Errorf("error = %q, want to contain 'do request'", err.Error())
	}
}

func TestCreateStreamDoRequestError(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:1"}
	_, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Errorf("error = %q, want to contain 'do request'", err.Error())
	}
}

func TestStreamRecvAfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}

	stream.Close()

	// Recv after close should return io.EOF.
	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("Recv after close = %v, want io.EOF", err)
	}
}

func TestStreamRecvBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprint(w, "data: not json\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode chunk") {
		t.Errorf("error = %q, want to contain 'decode chunk'", err.Error())
	}
}

func TestStreamRecvEOFOnEmptyStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Send nothing and close.
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("Recv = %v, want io.EOF", err)
	}
}

func TestStreamCloseNilResp(t *testing.T) {
	s := &Stream{}
	err := s.Close()
	if err != nil {
		t.Errorf("Close on nil resp = %v", err)
	}
}

func TestStreamRecvScannerError(t *testing.T) {
	// The scanner.Err() path is hit when the underlying reader returns an error
	// other than EOF. We simulate this with a handler that sends a very long line
	// exceeding the default scanner buffer.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// Write a "data: " prefix followed by a line exceeding the default 64KB scanner limit.
		// bufio.Scanner's default max token size is 64*1024.
		bigLine := "data: " + strings.Repeat("x", 70*1024) + "\n\n"
		w.Write([]byte(bigLine))
		flusher.Flush()
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error from scanner")
	}
	// Should be a scanner error (token too long) or similar.
}

func TestCreateInvalidURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid"}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q, want to contain 'new request'", err.Error())
	}
}

func TestCreateStreamInvalidURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid"}
	_, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q, want to contain 'new request'", err.Error())
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		resp := Response{ID: "1", Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL + "/"}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateMarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		return nil, fmt.Errorf("injected marshal error")
	}
	defer func() { jsonMarshal = orig }()

	client := &Client{BaseURL: "http://localhost"}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("error = %q, want to contain 'marshal request'", err.Error())
	}
}

func TestCreateStreamMarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		return nil, fmt.Errorf("injected marshal error")
	}
	defer func() { jsonMarshal = orig }()

	client := &Client{BaseURL: "http://localhost"}
	_, err := client.CreateStream(context.Background(), Request{
		Model:    "openclaw",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("error = %q, want to contain 'marshal request'", err.Error())
	}
}

func strPtr(s string) *string { return &s }
