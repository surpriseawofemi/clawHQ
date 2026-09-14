package chatcompletions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// jsonMarshal is used for JSON encoding; overridable in tests.
var jsonMarshal = json.Marshal

// Client calls the OpenClaw OpenAI-compatible Chat Completions endpoint.
type Client struct {
	// BaseURL is the gateway base URL (e.g. "http://127.0.0.1:18789").
	BaseURL string

	// Token is the bearer token for authentication.
	Token string

	// AgentID is the optional OpenClaw agent id (sent via x-openclaw-agent-id).
	AgentID string

	// SessionKey is the optional session key (sent via x-openclaw-session-key).
	SessionKey string

	// HTTPClient is the underlying HTTP client. If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/v1/chat/completions"
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.AgentID != "" {
		req.Header.Set("x-openclaw-agent-id", c.AgentID)
	}
	if c.SessionKey != "" {
		req.Header.Set("x-openclaw-session-key", c.SessionKey)
	}
}

// Create sends a non-streaming chat completion request.
func (c *Client) Create(ctx context.Context, req Request) (*Response, error) {
	req.Stream = false
	body, err := jsonMarshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, readHTTPError(httpResp)
	}

	var resp Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

// Stream represents an active SSE stream of chat completion chunks.
type Stream struct {
	resp    *http.Response
	scanner *bufio.Scanner
	closed  bool
}

// CreateStream sends a streaming chat completion request and returns a Stream.
// The caller must call Stream.Close when done.
func (c *Client) CreateStream(ctx context.Context, req Request) (*Stream, error) {
	req.Stream = true
	body, err := jsonMarshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	c.setHeaders(httpReq)

	httpResp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		defer httpResp.Body.Close()
		return nil, readHTTPError(httpResp)
	}

	return &Stream{
		resp:    httpResp,
		scanner: bufio.NewScanner(httpResp.Body),
	}, nil
}

// Recv reads the next StreamChunk from the SSE stream.
// Returns io.EOF when the stream ends (data: [DONE]).
func (s *Stream) Recv() (*StreamChunk, error) {
	if s.closed {
		return nil, io.EOF
	}
	for s.scanner.Scan() {
		line := s.scanner.Text()

		// SSE lines are "data: <json>" or "data: [DONE]"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return nil, io.EOF
		}
		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("decode chunk: %w", err)
		}
		return &chunk, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// Close closes the underlying HTTP response body.
func (s *Stream) Close() error {
	s.closed = true
	if s.resp != nil && s.resp.Body != nil {
		return s.resp.Body.Close()
	}
	return nil
}

// HTTPError represents a non-200 response from the gateway.
type HTTPError struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// Body is the response body truncated to 4096 bytes.
	Body       string
	// RetryAfter is the value of the Retry-After header, if present.
	RetryAfter string
}

func (e *HTTPError) Error() string {
	s := fmt.Sprintf("openclaw: HTTP %d: %s", e.StatusCode, e.Body)
	if e.RetryAfter != "" {
		s += " (retry-after: " + e.RetryAfter + ")"
	}
	return s
}

func readHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &HTTPError{
		StatusCode: resp.StatusCode,
		Body:       string(body),
		RetryAfter: resp.Header.Get("Retry-After"),
	}
}
