// Package toolsinvoke implements an HTTP client for the OpenClaw
// Tools Invoke endpoint (POST /tools/invoke).
//
// Reference: https://docs.openclaw.ai/gateway/tools-invoke-http-api
package toolsinvoke

import (
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

// Client calls the OpenClaw Tools Invoke HTTP endpoint.
type Client struct {
	// BaseURL is the gateway base URL (e.g. "http://127.0.0.1:18789").
	BaseURL string

	// Token is the bearer token for authentication.
	Token string

	// MessageChannel is the optional message channel hint
	// (sent via x-openclaw-message-channel).
	MessageChannel string

	// AccountID is the optional account id
	// (sent via x-openclaw-account-id).
	AccountID string

	// HTTPClient is the underlying HTTP client. If nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// Request is the body for POST /tools/invoke.
type Request struct {
	Tool       string         `json:"tool"`
	Action     string         `json:"action,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	SessionKey string         `json:"sessionKey,omitempty"`
	DryRun     bool           `json:"dryRun,omitempty"`
}

// Response is the parsed response from /tools/invoke.
type Response struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ErrorDetail    `json:"error,omitempty"`
}

// ErrorDetail carries error information from the gateway.
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) endpoint() string {
	return strings.TrimRight(c.BaseURL, "/") + "/tools/invoke"
}

// Invoke calls a single tool on the gateway.
func (c *Client) Invoke(ctx context.Context, req Request) (*Response, error) {
	body, err := jsonMarshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.MessageChannel != "" {
		httpReq.Header.Set("x-openclaw-message-channel", c.MessageChannel)
	}
	if c.AccountID != "" {
		httpReq.Header.Set("x-openclaw-account-id", c.AccountID)
	}

	httpResp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 2*1024*1024)) // 2 MB limit
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Handle non-JSON error responses.
	switch httpResp.StatusCode {
	case http.StatusUnauthorized:
		return nil, &HTTPError{StatusCode: 401, Body: string(respBody)}
	case http.StatusTooManyRequests:
		return nil, &HTTPError{
			StatusCode: 429,
			Body:       string(respBody),
			RetryAfter: httpResp.Header.Get("Retry-After"),
		}
	case http.StatusMethodNotAllowed:
		return nil, &HTTPError{StatusCode: 405, Body: string(respBody)}
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, &HTTPError{StatusCode: httpResp.StatusCode, Body: string(respBody)}
	}

	// 404 and 500 still have JSON bodies with ok:false.
	if !resp.OK && resp.Error != nil {
		return &resp, &InvokeError{
			StatusCode: httpResp.StatusCode,
			Type:       resp.Error.Type,
			Message:    resp.Error.Message,
		}
	}

	return &resp, nil
}

// HTTPError represents a non-JSON or transport-level error.
type HTTPError struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// Body is the raw response body.
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

// InvokeError represents a structured error from the tools/invoke endpoint.
type InvokeError struct {
	// StatusCode is the HTTP response status code.
	StatusCode int
	// Type is the machine-readable error type from the gateway.
	Type       string
	// Message is the human-readable error message from the gateway.
	Message    string
}

func (e *InvokeError) Error() string {
	return fmt.Sprintf("openclaw: invoke error (HTTP %d): %s: %s", e.StatusCode, e.Type, e.Message)
}
