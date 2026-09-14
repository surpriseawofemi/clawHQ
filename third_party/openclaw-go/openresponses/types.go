// Package openresponses implements an HTTP client for the OpenClaw
// OpenAI Responses API-compatible endpoint (POST /v1/responses).
//
// Reference: https://docs.openclaw.ai/gateway/openresponses-http-api
package openresponses

import "encoding/json"

// ---------------------------------------------------------------------------
// Request types
// ---------------------------------------------------------------------------

// Request is the body for POST /v1/responses.
type Request struct {
	// Model is required. Examples: "openclaw", "openclaw:main", "agent:beta".
	Model string `json:"model"`

	// Input is the conversation input — either a plain string or an array of
	// InputItem values. Use InputFromString or InputFromItems to construct.
	Input json.RawMessage `json:"input"`

	// Instructions is an optional system prompt merged into the conversation.
	Instructions string `json:"instructions,omitempty"`

	// Tools defines client tool (function) definitions.
	Tools []ToolDefinition `json:"tools,omitempty"`

	// ToolChoice controls tool selection: "auto", "none", "required", or
	// a specific function choice (use ToolChoiceFunction).
	ToolChoice any `json:"tool_choice,omitempty"`

	// Stream enables SSE streaming when true.
	Stream bool `json:"stream,omitempty"`

	// MaxOutputTokens is a best-effort output length limit.
	MaxOutputTokens *int `json:"max_output_tokens,omitempty"`

	// User is a stable session routing key.
	User string `json:"user,omitempty"`

	// Temperature is accepted but currently ignored by the gateway.
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP is accepted but currently ignored by the gateway.
	TopP *float64 `json:"top_p,omitempty"`

	// MaxToolCalls is accepted but currently ignored.
	MaxToolCalls *int `json:"max_tool_calls,omitempty"`

	// Metadata is arbitrary key-value metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Store is accepted but currently ignored.
	Store *bool `json:"store,omitempty"`

	// PreviousResponseID links to a prior response for multi-turn.
	PreviousResponseID string `json:"previous_response_id,omitempty"`

	// Reasoning controls reasoning effort/summary.
	Reasoning *Reasoning `json:"reasoning,omitempty"`

	// Truncation is "auto" or "disabled".
	Truncation string `json:"truncation,omitempty"`
}

// InputFromString creates a Request.Input from a plain string.
func InputFromString(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

// InputFromItems creates a Request.Input from an array of InputItem values.
func InputFromItems(items []InputItem) json.RawMessage {
	data, _ := json.Marshal(items)
	return data
}

// Reasoning controls the model's reasoning behavior.
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`  // "low", "medium", "high"
	Summary string `json:"summary,omitempty"` // "auto", "concise", "detailed"
}

// ToolDefinition defines a client function tool.
type ToolDefinition struct {
	Type     string       `json:"type"` // "function"
	Function FunctionTool `json:"function"`
}

// FunctionTool is the function specification inside a ToolDefinition.
type FunctionTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolChoiceFunction specifies a particular function for tool_choice.
type ToolChoiceFunction struct {
	Type     string                     `json:"type"` // "function"
	Function ToolChoiceFunctionSelector `json:"function"`
}

// ToolChoiceFunctionSelector names the function to call.
type ToolChoiceFunctionSelector struct {
	Name string `json:"name"`
}

// ---------------------------------------------------------------------------
// Input item types (discriminated union on Type)
// ---------------------------------------------------------------------------

// InputItem is a single input item. Set Type to select the variant.
// Only fields relevant to the chosen type should be populated.
type InputItem struct {
	Type string `json:"type"` // "message", "function_call", "function_call_output", "reasoning", "item_reference"

	// message fields
	Role    string          `json:"role,omitempty"`    // "system", "developer", "user", "assistant"
	Content json.RawMessage `json:"content,omitempty"` // string or []ContentPart

	// function_call fields
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// function_call_output fields
	Output string `json:"output,omitempty"`

	// reasoning fields
	EncryptedContent string `json:"encrypted_content,omitempty"`
	Summary          string `json:"summary,omitempty"`
}

// MessageItem creates a message InputItem with string content.
func MessageItem(role, content string) InputItem {
	data, _ := json.Marshal(content)
	return InputItem{Type: "message", Role: role, Content: data}
}

// MessageItemParts creates a message InputItem with structured content parts.
func MessageItemParts(role string, parts []ContentPart) InputItem {
	data, _ := json.Marshal(parts)
	return InputItem{Type: "message", Role: role, Content: data}
}

// FunctionCallItem creates a function_call InputItem.
func FunctionCallItem(callID, name, arguments string) InputItem {
	return InputItem{Type: "function_call", CallID: callID, Name: name, Arguments: arguments}
}

// FunctionCallOutputItem creates a function_call_output InputItem.
func FunctionCallOutputItem(callID, output string) InputItem {
	return InputItem{Type: "function_call_output", CallID: callID, Output: output}
}

// ---------------------------------------------------------------------------
// Content part types
// ---------------------------------------------------------------------------

// ContentPart is a part of a message's content array.
type ContentPart struct {
	Type string `json:"type"` // "input_text", "output_text", "input_image", "input_file"

	// text fields (input_text, output_text)
	Text string `json:"text,omitempty"`

	// image/file fields (input_image, input_file)
	Source *ContentSource `json:"source,omitempty"`
}

// ContentSource describes the source of an image or file.
type ContentSource struct {
	Type      string `json:"type"`                 // "url" or "base64"
	URL       string `json:"url,omitempty"`        // for type "url"
	MediaType string `json:"media_type,omitempty"` // for type "base64"
	Data      string `json:"data,omitempty"`       // for type "base64"
	Filename  string `json:"filename,omitempty"`   // optional, for input_file base64
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

// Response is the response resource from POST /v1/responses.
type Response struct {
	ID        string       `json:"id"`         // "resp_<uuid>"
	Object    string       `json:"object"`     // "response"
	CreatedAt int64        `json:"created_at"` // Unix epoch seconds
	Status    string       `json:"status"`     // "in_progress", "completed", "failed", "cancelled", "incomplete"
	Model     string       `json:"model"`
	Output    []OutputItem `json:"output"`
	Usage     Usage        `json:"usage"`
	Error     *ErrorInfo   `json:"error,omitempty"`
}

// Usage reports token counts.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ErrorInfo describes an error in a failed response.
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OutputItem is a single item in the response output array.
// Discriminated on Type.
type OutputItem struct {
	Type string `json:"type"` // "message", "function_call", "reasoning"

	// Common
	ID     string `json:"id"`
	Status string `json:"status,omitempty"` // "in_progress", "completed"

	// message fields
	Role    string       `json:"role,omitempty"`
	Content []OutputText `json:"content,omitempty"`

	// function_call fields
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// reasoning fields (not currently emitted)
	Summary string `json:"summary,omitempty"`
}

// OutputText is a text content part in an output message.
type OutputText struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Streaming event types (SSE)
// ---------------------------------------------------------------------------

// StreamEvent is a typed SSE event from a streaming response.
// The EventType field corresponds to the SSE "event:" line.
type StreamEvent struct {
	EventType string          `json:"-"`    // set from SSE event line
	Type      string          `json:"type"` // matches EventType
	RawData   json.RawMessage `json:"-"`    // full JSON data line
}

// ResponseEvent carries a full Response resource. Used by:
// response.created, response.in_progress, response.completed, response.failed.
type ResponseEvent struct {
	Type     string   `json:"type"`
	Response Response `json:"response"`
}

// OutputItemEvent carries an output item with its index. Used by:
// response.output_item.added, response.output_item.done.
type OutputItemEvent struct {
	Type        string     `json:"type"`
	OutputIndex int        `json:"output_index"`
	Item        OutputItem `json:"item"`
}

// ContentPartEvent carries a content part with indices. Used by:
// response.content_part.added, response.content_part.done.
type ContentPartEvent struct {
	Type         string     `json:"type"`
	ItemID       string     `json:"item_id"`
	OutputIndex  int        `json:"output_index"`
	ContentIndex int        `json:"content_index"`
	Part         OutputText `json:"part"`
}

// OutputTextDeltaEvent carries a text delta chunk.
type OutputTextDeltaEvent struct {
	Type         string `json:"type"` // "response.output_text.delta"
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

// OutputTextDoneEvent carries the completed text.
type OutputTextDoneEvent struct {
	Type         string `json:"type"` // "response.output_text.done"
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

// APIError is the error envelope returned by the gateway for non-200 responses.
type APIError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}
