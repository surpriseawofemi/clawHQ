package toolsinvoke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInvokeSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/tools/invoke" {
			t.Errorf("path = %s, want /tools/invoke", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-openclaw-message-channel") != "slack" {
			t.Errorf("channel = %q, want 'slack'", r.Header.Get("x-openclaw-message-channel"))
		}

		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Tool != "sessions_list" {
			t.Errorf("tool = %q, want 'sessions_list'", req.Tool)
		}
		if req.Action != "json" {
			t.Errorf("action = %q, want 'json'", req.Action)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Response{
			OK:     true,
			Result: json.RawMessage(`[{"key":"main"}]`),
		})
	}))
	defer srv.Close()

	client := &Client{
		BaseURL:        srv.URL,
		Token:          "test-tok",
		MessageChannel: "slack",
	}

	resp, err := client.Invoke(context.Background(), Request{
		Tool:   "sessions_list",
		Action: "json",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false, want true")
	}
	if string(resp.Result) != `[{"key":"main"}]` {
		t.Errorf("result = %s", resp.Result)
	}
}

func TestInvokeWithAccountID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-openclaw-account-id") != "acct-1" {
			t.Errorf("account-id = %q, want 'acct-1'", r.Header.Get("x-openclaw-account-id"))
		}
		json.NewEncoder(w).Encode(Response{OK: true, Result: json.RawMessage(`"ok"`)})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok", AccountID: "acct-1"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
}

func TestInvokeNoTokenNoHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("auth should be empty, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-openclaw-message-channel") != "" {
			t.Errorf("channel should be empty, got %q", r.Header.Get("x-openclaw-message-channel"))
		}
		if r.Header.Get("x-openclaw-account-id") != "" {
			t.Errorf("account-id should be empty, got %q", r.Header.Get("x-openclaw-account-id"))
		}
		json.NewEncoder(w).Encode(Response{OK: true, Result: json.RawMessage(`"ok"`)})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
}

func TestInvokeWithCustomHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Response{OK: true, Result: json.RawMessage(`"ok"`)})
	}))
	defer srv.Close()

	client := &Client{
		BaseURL:    srv.URL,
		Token:      "tok",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
	resp, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false")
	}
}

func TestInvokeToolNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(Response{
			OK:    false,
			Error: &ErrorDetail{Type: "NOT_FOUND", Message: "tool not available"},
		})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Invoke(context.Background(), Request{Tool: "nonexistent"})
	if err == nil {
		t.Fatal("expected error")
	}
	var invErr *InvokeError
	if !errors.As(err, &invErr) {
		t.Fatalf("expected InvokeError, got %T: %v", err, err)
	}
	if invErr.StatusCode != 404 {
		t.Errorf("status = %d, want 404", invErr.StatusCode)
	}
	if invErr.Type != "NOT_FOUND" {
		t.Errorf("type = %q, want 'NOT_FOUND'", invErr.Type)
	}
}

func TestInvokeUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 401 {
		t.Errorf("status = %d, want 401", httpErr.StatusCode)
	}
}

func TestInvokeRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 429 {
		t.Errorf("status = %d, want 429", httpErr.StatusCode)
	}
	if httpErr.RetryAfter != "30" {
		t.Errorf("retry-after = %q, want '30'", httpErr.RetryAfter)
	}
}

func TestInvokeMethodNotAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("method not allowed"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 405 {
		t.Errorf("status = %d, want 405", httpErr.StatusCode)
	}
}

func TestInvokeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(Response{
			OK:    false,
			Error: &ErrorDetail{Type: "INTERNAL", Message: "something broke"},
		})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	var invErr *InvokeError
	if !errors.As(err, &invErr) {
		t.Fatalf("expected InvokeError, got %T: %v", err, err)
	}
	if invErr.StatusCode != 500 {
		t.Errorf("status = %d, want 500", invErr.StatusCode)
	}
}

func TestInvokeWithSessionKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if req.SessionKey != "custom" {
			t.Errorf("sessionKey = %q, want 'custom'", req.SessionKey)
		}
		json.NewEncoder(w).Encode(Response{OK: true, Result: json.RawMessage(`"ok"`)})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	resp, err := client.Invoke(context.Background(), Request{
		Tool:       "sessions_list",
		SessionKey: "custom",
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !resp.OK {
		t.Error("ok = false")
	}
}

func TestInvokeDoRequestError(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:1"} // nothing listening
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Errorf("error = %q, want to contain 'do request'", err.Error())
	}
}

func TestInvokeNonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 200 {
		t.Errorf("status = %d, want 200", httpErr.StatusCode)
	}
}

func TestHTTPErrorString(t *testing.T) {
	e := &HTTPError{StatusCode: 429, Body: "rate limited", RetryAfter: "60"}
	s := e.Error()
	if !strings.Contains(s, "429") {
		t.Errorf("Error() = %q, want to contain '429'", s)
	}
	if !strings.Contains(s, "retry-after: 60") {
		t.Errorf("Error() = %q, want to contain 'retry-after: 60'", s)
	}
}

func TestHTTPErrorStringNoRetryAfter(t *testing.T) {
	e := &HTTPError{StatusCode: 401, Body: "unauthorized"}
	s := e.Error()
	if strings.Contains(s, "retry-after") {
		t.Errorf("Error() = %q, should not contain 'retry-after'", s)
	}
	if !strings.Contains(s, "401") {
		t.Errorf("Error() = %q, want to contain '401'", s)
	}
}

func TestInvokeErrorString(t *testing.T) {
	e := &InvokeError{StatusCode: 404, Type: "NOT_FOUND", Message: "tool not available"}
	s := e.Error()
	if !strings.Contains(s, "404") || !strings.Contains(s, "NOT_FOUND") || !strings.Contains(s, "tool not available") {
		t.Errorf("Error() = %q", s)
	}
}

func TestInvokeInvalidURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q, want to contain 'new request'", err.Error())
	}
}

func TestInvokeMarshalError(t *testing.T) {
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) {
		return nil, fmt.Errorf("injected marshal error")
	}
	defer func() { jsonMarshal = orig }()

	client := &Client{BaseURL: "http://localhost"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("error = %q, want to contain 'marshal request'", err.Error())
	}
}

func TestInvokeReadResponseError(t *testing.T) {
	// The server sends a partial response then closes, which can cause
	// io.ReadAll to return an error on certain conditions.
	// Actually, io.ReadAll will return whatever was read even on error in many cases.
	// A more reliable way: use a custom HTTPClient with a transport that returns an error body.
	client := &Client{
		BaseURL: "http://localhost",
		HTTPClient: &http.Client{
			Transport: &errorBodyTransport{},
		},
	}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "read response") {
		t.Errorf("error = %q, want to contain 'read response'", err.Error())
	}
}

// errorBodyTransport returns an HTTP response with a body that errors on read.
type errorBodyTransport struct{}

func (t *errorBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       &errorReader{},
	}, nil
}

// errorReader is a reader that always returns an error.
type errorReader struct{}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("injected read error")
}

func (r *errorReader) Close() error { return nil }

func TestBaseURLTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/invoke" {
			t.Errorf("path = %s, want /tools/invoke", r.URL.Path)
		}
		json.NewEncoder(w).Encode(Response{OK: true, Result: json.RawMessage(`"ok"`)})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL + "/", Token: "tok"}
	_, err := client.Invoke(context.Background(), Request{Tool: "test"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
}
