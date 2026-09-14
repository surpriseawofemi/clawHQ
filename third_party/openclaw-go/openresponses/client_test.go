package openresponses

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

// --- Create (non-streaming) ---

func TestCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %s, want /v1/responses", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-openclaw-agent-id") != "main" {
			t.Errorf("agent-id = %q", r.Header.Get("x-openclaw-agent-id"))
		}

		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if req.Model != "openclaw:main" {
			t.Errorf("model = %q", req.Model)
		}

		resp := Response{
			ID:        "resp_123",
			Object:    "response",
			CreatedAt: time.Now().Unix(),
			Status:    "completed",
			Model:     "openclaw:main",
			Output: []OutputItem{{
				Type:   "message",
				ID:     "msg_1",
				Role:   "assistant",
				Status: "completed",
				Content: []OutputText{{
					Type: "output_text",
					Text: "Hello!",
				}},
			}},
			Usage: Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
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
		Model: "openclaw:main",
		Input: InputFromString("Hello"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID != "resp_123" {
		t.Errorf("id = %q", resp.ID)
	}
	if resp.Status != "completed" {
		t.Errorf("status = %q", resp.Status)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("output len = %d", len(resp.Output))
	}
	if resp.Output[0].Content[0].Text != "Hello!" {
		t.Errorf("text = %q", resp.Output[0].Content[0].Text)
	}
	if resp.Usage.TotalTokens != 8 {
		t.Errorf("total_tokens = %d", resp.Usage.TotalTokens)
	}
}

func TestCreateWithItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)

		var items []InputItem
		if err := json.Unmarshal(req.Input, &items); err != nil {
			t.Fatalf("unmarshal items: %v", err)
		}
		if len(items) != 2 {
			t.Errorf("items len = %d, want 2", len(items))
		}

		resp := Response{ID: "resp_2", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromItems([]InputItem{
			MessageItem("system", "You are helpful"),
			MessageItem("user", "Hi"),
		}),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateMetadataStripping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "\"metadata\"") {
			t.Errorf("metadata should be omitted when empty: %s", string(body))
		}
		resp := Response{ID: "resp_meta", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Create(context.Background(), Request{
		Model:    "openclaw",
		Input:    InputFromString("hi"),
		Metadata: map[string]string{},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateWithToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{
			ID:     "resp_3",
			Object: "response",
			Status: "incomplete",
			Output: []OutputItem{{
				Type:      "function_call",
				ID:        "call_1",
				CallID:    "call_abc",
				Name:      "get_weather",
				Arguments: `{"location":"SF"}`,
				Status:    "completed",
			}},
			Usage: Usage{InputTokens: 10, OutputTokens: 15, TotalTokens: 25},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	resp, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("What's the weather in SF?"),
		Tools: []ToolDefinition{{
			Type: "function",
			Function: FunctionTool{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
		ToolChoice: "auto",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.Status != "incomplete" {
		t.Errorf("status = %q", resp.Status)
	}
	if resp.Output[0].Name != "get_weather" {
		t.Errorf("tool name = %q", resp.Output[0].Name)
	}
}

func TestCreateWithToolChoiceFunction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		var tc ToolChoiceFunction
		if err := json.Unmarshal(raw["tool_choice"], &tc); err != nil {
			t.Fatalf("unmarshal tool_choice: %v", err)
		}
		if tc.Function.Name != "get_weather" {
			t.Errorf("tool_choice.function.name = %q", tc.Function.Name)
		}

		resp := Response{ID: "resp_4", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("test"),
		ToolChoice: ToolChoiceFunction{
			Type:     "function",
			Function: ToolChoiceFunctionSelector{Name: "get_weather"},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateWithCustomHTTPClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := Response{ID: "resp_5", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{
		BaseURL:    srv.URL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
	resp, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.ID != "resp_5" {
		t.Errorf("id = %q", resp.ID)
	}
}

func TestCreateNoTokenNoAgentNoSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("auth should be empty")
		}
		if r.Header.Get("x-openclaw-agent-id") != "" {
			t.Errorf("agent-id should be empty")
		}
		if r.Header.Get("x-openclaw-session-key") != "" {
			t.Errorf("session-key should be empty")
		}
		resp := Response{ID: "resp_6", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestSessionKeyHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-openclaw-session-key") != "custom-session" {
			t.Errorf("session key = %q", r.Header.Get("x-openclaw-session-key"))
		}
		resp := Response{ID: "resp_7", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok", SessionKey: "custom-session"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestCreateHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"unauthorized","type":"unauthorized"}}`))
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 401 {
		t.Errorf("status = %d", httpErr.StatusCode)
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
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestCreateDoRequestError(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:1"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestCreateInvalidURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q", err.Error())
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
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %s", r.URL.Path)
		}
		resp := Response{ID: "resp_8", Object: "response", Status: "completed"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL + "/"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
}

// --- CreateStream ---

func TestCreateStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("stream = false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// response.created
		fmt.Fprint(w, "event: response.created\n")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(ResponseEvent{
			Type:     "response.created",
			Response: Response{ID: "resp_s1", Object: "response", Status: "in_progress"},
		}))
		flusher.Flush()

		// response.output_text.delta
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(OutputTextDeltaEvent{
			Type: "response.output_text.delta", ItemID: "msg_1", Delta: "Hello",
		}))
		flusher.Flush()

		// response.output_text.delta (second chunk)
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(OutputTextDeltaEvent{
			Type: "response.output_text.delta", ItemID: "msg_1", Delta: "!",
		}))
		flusher.Flush()

		// response.completed
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(ResponseEvent{
			Type: "response.completed",
			Response: Response{
				ID: "resp_s1", Object: "response", Status: "completed",
				Usage: Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8},
			},
		}))
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	stream, err := client.CreateStream(context.Background(), Request{
		Model: "openclaw:main",
		Input: InputFromString("Hi"),
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	var events []string
	var deltas []string
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		events = append(events, ev.EventType)

		if ev.EventType == "response.output_text.delta" {
			var delta OutputTextDeltaEvent
			json.Unmarshal(ev.RawData, &delta)
			deltas = append(deltas, delta.Delta)
		}
	}

	if len(events) != 4 {
		t.Fatalf("events = %d, want 4: %v", len(events), events)
	}
	if events[0] != "response.created" {
		t.Errorf("first event = %q", events[0])
	}
	if len(deltas) != 2 || deltas[0] != "Hello" || deltas[1] != "!" {
		t.Errorf("deltas = %v", deltas)
	}
}

func TestCreateStreamNoEventLine(t *testing.T) {
	// Test SSE where there is no "event:" line, just "data:" with a type field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Send data without an event: line.
		fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.created","response":{"id":"r1","object":"response","status":"in_progress"}}`)
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	ev, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev.EventType != "response.created" {
		t.Errorf("event type = %q", ev.EventType)
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
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 401 {
		t.Errorf("status = %d", httpErr.StatusCode)
	}
}

func TestCreateStreamDoRequestError(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:1"}
	_, err := client.CreateStream(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "do request") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestCreateStreamInvalidURL(t *testing.T) {
	client := &Client{BaseURL: "://invalid"}
	_, err := client.CreateStream(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "new request") {
		t.Errorf("error = %q", err.Error())
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
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "marshal request") {
		t.Errorf("error = %q", err.Error())
	}
}

// --- Stream methods ---

func TestStreamRecvAfterClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}

	stream.Close()

	_, err = stream.Recv()
	if err != io.EOF {
		t.Errorf("Recv after close = %v, want io.EOF", err)
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
		Model: "openclaw",
		Input: InputFromString("hi"),
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

func TestStreamRecvScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		bigLine := "data: " + strings.Repeat("x", 70*1024) + "\n\n"
		w.Write([]byte(bigLine))
		flusher.Flush()
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL}
	stream, err := client.CreateStream(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	defer stream.Close()

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error from scanner")
	}
}

func TestStreamCloseNilResp(t *testing.T) {
	s := &Stream{}
	if err := s.Close(); err != nil {
		t.Errorf("Close on nil resp = %v", err)
	}
}

// --- HTTPError ---

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
	_, err := c.Create(context.Background(), Request{Model: "test", Input: InputFromString("hi")})
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
	_, err := c.CreateStream(context.Background(), Request{Model: "test", Input: InputFromString("hi")})
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

// --- Type construction helpers ---

func TestInputFromString(t *testing.T) {
	raw := InputFromString("hello")
	if string(raw) != `"hello"` {
		t.Errorf("InputFromString = %s", raw)
	}
}

func TestInputFromItems(t *testing.T) {
	raw := InputFromItems([]InputItem{
		MessageItem("user", "hi"),
		FunctionCallOutputItem("call_1", "result"),
	})
	var items []InputItem
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	if items[0].Type != "message" || items[0].Role != "user" {
		t.Errorf("item[0] = %+v", items[0])
	}
	if items[1].Type != "function_call_output" || items[1].CallID != "call_1" {
		t.Errorf("item[1] = %+v", items[1])
	}
}

func TestMessageItem(t *testing.T) {
	item := MessageItem("user", "hello")
	if item.Type != "message" || item.Role != "user" {
		t.Errorf("item = %+v", item)
	}
	var content string
	if err := json.Unmarshal(item.Content, &content); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if content != "hello" {
		t.Errorf("content = %q", content)
	}
}

func TestMessageItemParts(t *testing.T) {
	item := MessageItemParts("user", []ContentPart{
		{Type: "input_text", Text: "hello"},
		{Type: "input_image", Source: &ContentSource{Type: "url", URL: "https://example.com/img.png"}},
	})
	if item.Type != "message" || item.Role != "user" {
		t.Errorf("item = %+v", item)
	}
	var parts []ContentPart
	if err := json.Unmarshal(item.Content, &parts); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d", len(parts))
	}
	if parts[1].Source.URL != "https://example.com/img.png" {
		t.Errorf("source url = %q", parts[1].Source.URL)
	}
}

func TestFunctionCallItem(t *testing.T) {
	item := FunctionCallItem("call_1", "get_weather", `{"location":"SF"}`)
	if item.Type != "function_call" || item.CallID != "call_1" || item.Name != "get_weather" {
		t.Errorf("item = %+v", item)
	}
}

func TestFunctionCallOutputItem(t *testing.T) {
	item := FunctionCallOutputItem("call_1", "sunny")
	if item.Type != "function_call_output" || item.CallID != "call_1" || item.Output != "sunny" {
		t.Errorf("item = %+v", item)
	}
}

// --- Type serialization round trips ---

func TestResponseRoundTrip(t *testing.T) {
	resp := Response{
		ID:        "resp_abc",
		Object:    "response",
		CreatedAt: 1700000000,
		Status:    "completed",
		Model:     "openclaw",
		Output: []OutputItem{
			{Type: "message", ID: "msg_1", Role: "assistant", Status: "completed",
				Content: []OutputText{{Type: "output_text", Text: "Hello"}}},
			{Type: "function_call", ID: "call_1", CallID: "c1", Name: "tool", Arguments: "{}"},
		},
		Usage: Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		Error: &ErrorInfo{Code: "test", Message: "test error"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Response
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != resp.ID || got.Status != resp.Status || got.Error.Code != "test" {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if len(got.Output) != 2 || got.Output[1].Name != "tool" {
		t.Errorf("output mismatch: %+v", got.Output)
	}
}

func TestStreamEventTypes(t *testing.T) {
	// Verify all streaming event types unmarshal correctly.
	tests := []struct {
		name string
		data string
		want string
	}{
		{"ResponseEvent", `{"type":"response.created","response":{"id":"r1","object":"response","status":"in_progress","model":"m","created_at":1,"output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`, "response.created"},
		{"OutputItemEvent", `{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1"}}`, "response.output_item.added"},
		{"ContentPartEvent", `{"type":"response.content_part.added","item_id":"m1","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`, "response.content_part.added"},
		{"OutputTextDelta", `{"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"hi"}`, "response.output_text.delta"},
		{"OutputTextDone", `{"type":"response.output_text.done","item_id":"m1","output_index":0,"content_index":0,"text":"hi"}`, "response.output_text.done"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var partial struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(tt.data), &partial); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if partial.Type != tt.want {
				t.Errorf("type = %q, want %q", partial.Type, tt.want)
			}
		})
	}
}

func TestAPIErrorRoundTrip(t *testing.T) {
	data := `{"error":{"message":"bad request","type":"invalid_request_error"}}`
	var apiErr APIError
	if err := json.Unmarshal([]byte(data), &apiErr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if apiErr.Error.Type != "invalid_request_error" {
		t.Errorf("type = %q", apiErr.Error.Type)
	}
}

func TestReasoningType(t *testing.T) {
	r := &Reasoning{Effort: "high", Summary: "concise"}
	data, _ := json.Marshal(r)
	var got Reasoning
	json.Unmarshal(data, &got)
	if got.Effort != "high" || got.Summary != "concise" {
		t.Errorf("reasoning = %+v", got)
	}
}

func TestContentSourceBase64(t *testing.T) {
	cs := ContentSource{
		Type:      "base64",
		MediaType: "image/png",
		Data:      "iVBORw0KGgo=",
		Filename:  "test.png",
	}
	data, _ := json.Marshal(cs)
	var got ContentSource
	json.Unmarshal(data, &got)
	if got.Type != "base64" || got.Filename != "test.png" {
		t.Errorf("source = %+v", got)
	}
}

func TestRequestAllFields(t *testing.T) {
	req := Request{
		Model:              "openclaw",
		Input:              InputFromString("test"),
		Instructions:       "be helpful",
		Stream:             true,
		MaxOutputTokens:    intPtr(100),
		User:               "user1",
		Temperature:        float64Ptr(0.7),
		TopP:               float64Ptr(0.9),
		MaxToolCalls:       intPtr(5),
		Metadata:           map[string]string{"key": "val"},
		Store:              boolPtr(true),
		PreviousResponseID: "resp_prev",
		Reasoning:          &Reasoning{Effort: "medium"},
		Truncation:         "auto",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Request
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Instructions != "be helpful" || got.User != "user1" || got.Truncation != "auto" {
		t.Errorf("request = %+v", got)
	}
	if got.PreviousResponseID != "resp_prev" {
		t.Errorf("previous_response_id = %q", got.PreviousResponseID)
	}
}

func TestCreateWithFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(Response{
			ID:     "resp_fail",
			Object: "response",
			Status: "failed",
			Error:  &ErrorInfo{Code: "api_error", Message: "internal error"},
		})
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL, Token: "tok"}
	_, err := client.Create(context.Background(), Request{
		Model: "openclaw",
		Input: InputFromString("hi"),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 500 {
		t.Errorf("status = %d", httpErr.StatusCode)
	}
}

// --- helpers ---

func mustJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func intPtr(i int) *int             { return &i }
func float64Ptr(f float64) *float64 { return &f }
func boolPtr(b bool) *bool          { return &b }
