// Package acp implements the Agent Client Protocol (ACP) for bridging
// between code editors (IDE clients) and an OpenClaw Gateway agent.
//
// ACP uses JSON-RPC 2.0 over NDJSON (newline-delimited JSON) on stdio.
// The IDE spawns the agent process and communicates via stdin/stdout.
//
// Reference: https://agentclientprotocol.com
package acp

import "encoding/json"

// ProtocolVersion is the ACP protocol version.
const ProtocolVersion = 1

// ---------------------------------------------------------------------------
// JSON-RPC 2.0
// ---------------------------------------------------------------------------

// RPCRequest is a JSON-RPC 2.0 request.
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"` // string or number; nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCResponse is a JSON-RPC 2.0 response.
type RPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ErrCodeParseError     = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// Extended error codes.
const (
	ErrCodeRequestCancelled     = -32800 // UNSTABLE
	ErrCodeAuthenticationNeeded = -32000
	ErrCodeResourceNotFound     = -32002
)

// ---------------------------------------------------------------------------
// Common types
// ---------------------------------------------------------------------------

// Meta is the optional _meta field present on all ACP types.
type Meta = map[string]any

// Role is "assistant" or "user".
type Role = string

// StopReason enumerates prompt stop reasons.
const (
	StopReasonEndTurn         = "end_turn"
	StopReasonMaxTokens       = "max_tokens"
	StopReasonMaxTurnRequests = "max_turn_requests"
	StopReasonRefusal         = "refusal"
	StopReasonCancelled       = "cancelled"
)

// ToolCallStatus enumerates tool call statuses.
const (
	ToolCallStatusPending    = "pending"
	ToolCallStatusInProgress = "in_progress"
	ToolCallStatusCompleted  = "completed"
	ToolCallStatusFailed     = "failed"
)

// ToolKind enumerates tool kinds.
const (
	ToolKindRead       = "read"
	ToolKindEdit       = "edit"
	ToolKindDelete     = "delete"
	ToolKindMove       = "move"
	ToolKindSearch     = "search"
	ToolKindExecute    = "execute"
	ToolKindThink      = "think"
	ToolKindFetch      = "fetch"
	ToolKindSwitchMode = "switch_mode"
	ToolKindOther      = "other"
)

// PermissionOptionKind enumerates permission option kinds.
const (
	PermissionOptionKindAllowOnce    = "allow_once"
	PermissionOptionKindAllowAlways  = "allow_always"
	PermissionOptionKindRejectOnce   = "reject_once"
	PermissionOptionKindRejectAlways = "reject_always"
)

// PlanEntryPriority enumerates plan entry priorities.
const (
	PlanEntryPriorityHigh   = "high"
	PlanEntryPriorityMedium = "medium"
	PlanEntryPriorityLow    = "low"
)

// PlanEntryStatus enumerates plan entry statuses.
const (
	PlanEntryStatusPending    = "pending"
	PlanEntryStatusInProgress = "in_progress"
	PlanEntryStatusCompleted  = "completed"
)

// SessionConfigOptionCategory well-known categories.
const (
	SessionConfigCategoryMode         = "mode"
	SessionConfigCategoryModel        = "model"
	SessionConfigCategoryThoughtLevel = "thought_level"
)

// ---------------------------------------------------------------------------
// Annotations
// ---------------------------------------------------------------------------

// Annotations are optional metadata on content types.
type Annotations struct {
	Meta         Meta     `json:"_meta,omitempty"`
	Audience     []Role   `json:"audience,omitempty"`
	LastModified *string  `json:"lastModified,omitempty"`
	Priority     *float64 `json:"priority,omitempty"`
}

// ---------------------------------------------------------------------------
// Implementation (shared by ClientInfo and AgentInfo)
// ---------------------------------------------------------------------------

// Implementation identifies a client or agent.
type Implementation struct {
	Meta    Meta    `json:"_meta,omitempty"`
	Name    string  `json:"name"`
	Title   *string `json:"title,omitempty"`
	Version string  `json:"version"`
}

// ---------------------------------------------------------------------------
// Initialize
// ---------------------------------------------------------------------------

// InitializeRequest is the params for the "initialize" method.
type InitializeRequest struct {
	Meta               Meta                `json:"_meta,omitempty"`
	ProtocolVersion    int                 `json:"protocolVersion"`
	ClientCapabilities *ClientCapabilities `json:"clientCapabilities,omitempty"`
	ClientInfo         *Implementation     `json:"clientInfo,omitempty"`
}

// ClientCapabilities describes what the IDE client supports.
type ClientCapabilities struct {
	Meta     Meta                  `json:"_meta,omitempty"`
	FS       *FileSystemCapability `json:"fs,omitempty"`
	Terminal bool                  `json:"terminal,omitempty"`
}

// FileSystemCapability describes filesystem operations the client supports.
type FileSystemCapability struct {
	Meta          Meta `json:"_meta,omitempty"`
	ReadTextFile  bool `json:"readTextFile,omitempty"`
	WriteTextFile bool `json:"writeTextFile,omitempty"`
}

// InitializeResponse is the result of the "initialize" method.
type InitializeResponse struct {
	Meta              Meta               `json:"_meta,omitempty"`
	ProtocolVersion   int                `json:"protocolVersion"`
	AgentCapabilities *AgentCapabilities `json:"agentCapabilities,omitempty"`
	AgentInfo         *Implementation    `json:"agentInfo,omitempty"`
	AuthMethods       []AuthMethod       `json:"authMethods,omitempty"`
}

// AgentCapabilities describes what the agent server supports.
type AgentCapabilities struct {
	Meta                Meta                 `json:"_meta,omitempty"`
	LoadSession         bool                 `json:"loadSession,omitempty"`
	PromptCapabilities  *PromptCapabilities  `json:"promptCapabilities,omitempty"`
	MCPCapabilities     *MCPCapabilities     `json:"mcpCapabilities,omitempty"`
	SessionCapabilities *SessionCapabilities `json:"sessionCapabilities,omitempty"`
}

// PromptCapabilities describes prompt media support.
type PromptCapabilities struct {
	Meta            Meta `json:"_meta,omitempty"`
	Audio           bool `json:"audio,omitempty"`
	EmbeddedContext bool `json:"embeddedContext,omitempty"`
	Image           bool `json:"image,omitempty"`
}

// MCPCapabilities describes MCP server support.
type MCPCapabilities struct {
	Meta Meta `json:"_meta,omitempty"`
	HTTP bool `json:"http,omitempty"`
	SSE  bool `json:"sse,omitempty"`
}

// SessionCapabilities describes session management features.
type SessionCapabilities struct {
	Meta   Meta                       `json:"_meta,omitempty"`
	Fork   *SessionForkCapabilities   `json:"fork,omitempty"`
	List   *SessionListCapabilities   `json:"list,omitempty"`
	Resume *SessionResumeCapabilities `json:"resume,omitempty"`
}

// SessionForkCapabilities is a marker for fork support.
type SessionForkCapabilities struct {
	Meta Meta `json:"_meta,omitempty"`
}

// SessionListCapabilities is a marker for list support.
type SessionListCapabilities struct {
	Meta Meta `json:"_meta,omitempty"`
}

// SessionResumeCapabilities is a marker for resume support.
type SessionResumeCapabilities struct {
	Meta Meta `json:"_meta,omitempty"`
}

// AuthMethod describes an authentication method.
type AuthMethod struct {
	Meta        Meta    `json:"_meta,omitempty"`
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// ---------------------------------------------------------------------------
// Authenticate
// ---------------------------------------------------------------------------

// AuthenticateRequest is the params for the "authenticate" method.
type AuthenticateRequest struct {
	Meta     Meta   `json:"_meta,omitempty"`
	MethodID string `json:"methodId"`
}

// AuthenticateResponse is the result of the "authenticate" method.
type AuthenticateResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

// ---------------------------------------------------------------------------
// MCP Server configuration
// ---------------------------------------------------------------------------

// MCPServer is a union of McpServerStdio, McpServerHttp, and McpServerSse.
// Use the Type field to discriminate: "http", "sse", or "" (stdio).
type MCPServer struct {
	Meta Meta   `json:"_meta,omitempty"`
	Type string `json:"type,omitempty"` // "http", "sse", or "" for stdio

	// Common fields.
	Name string `json:"name"`

	// Stdio fields.
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`

	// HTTP/SSE fields.
	URL     string       `json:"url,omitempty"`
	Headers []HTTPHeader `json:"headers,omitempty"`
}

// HTTPHeader is a name/value pair for HTTP headers.
type HTTPHeader struct {
	Meta  Meta   `json:"_meta,omitempty"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// EnvVariable is a name/value pair for environment variables.
type EnvVariable struct {
	Meta  Meta   `json:"_meta,omitempty"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ---------------------------------------------------------------------------
// Session management
// ---------------------------------------------------------------------------

// NewSessionRequest is the params for "session/new".
type NewSessionRequest struct {
	Meta       Meta        `json:"_meta,omitempty"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// NewSessionResponse is the result of "session/new".
type NewSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// LoadSessionRequest is the params for "session/load".
type LoadSessionRequest struct {
	Meta       Meta        `json:"_meta,omitempty"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// LoadSessionResponse is the result of "session/load".
type LoadSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// ListSessionsRequest is the params for "session/list" (UNSTABLE).
type ListSessionsRequest struct {
	Meta   Meta    `json:"_meta,omitempty"`
	Cursor *string `json:"cursor,omitempty"`
	CWD    *string `json:"cwd,omitempty"`
}

// ListSessionsResponse is the result of "session/list" (UNSTABLE).
type ListSessionsResponse struct {
	Meta       Meta          `json:"_meta,omitempty"`
	Sessions   []SessionInfo `json:"sessions"`
	NextCursor *string       `json:"nextCursor,omitempty"`
}

// SessionInfo describes a session in the list (UNSTABLE).
type SessionInfo struct {
	Meta      Meta    `json:"_meta,omitempty"`
	SessionID string  `json:"sessionId"`
	CWD       string  `json:"cwd"`
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`
}

// ForkSessionRequest is the params for "session/fork" (UNSTABLE).
type ForkSessionRequest struct {
	Meta       Meta        `json:"_meta,omitempty"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

// ForkSessionResponse is the result of "session/fork" (UNSTABLE).
type ForkSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	SessionID     string                `json:"sessionId"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// ResumeSessionRequest is the params for "session/resume" (UNSTABLE).
type ResumeSessionRequest struct {
	Meta       Meta        `json:"_meta,omitempty"`
	SessionID  string      `json:"sessionId"`
	CWD        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers,omitempty"`
}

// ResumeSessionResponse is the result of "session/resume" (UNSTABLE).
type ResumeSessionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	Modes         *SessionModeState     `json:"modes,omitempty"`
	Models        *SessionModelState    `json:"models,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`
}

// SetSessionModeRequest is the params for "session/set_mode".
type SetSessionModeRequest struct {
	Meta      Meta   `json:"_meta,omitempty"`
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// SetSessionModeResponse is the result of "session/set_mode".
type SetSessionModeResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

// SetSessionModelRequest is the params for "session/set_model" (UNSTABLE).
type SetSessionModelRequest struct {
	Meta      Meta   `json:"_meta,omitempty"`
	SessionID string `json:"sessionId"`
	ModelID   string `json:"modelId"`
}

// SetSessionModelResponse is the result of "session/set_model" (UNSTABLE).
type SetSessionModelResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

// SetSessionConfigOptionRequest is the params for "session/set_config_option".
type SetSessionConfigOptionRequest struct {
	Meta      Meta   `json:"_meta,omitempty"`
	SessionID string `json:"sessionId"`
	ConfigID  string `json:"configId"`
	Value     string `json:"value"`
}

// SetSessionConfigOptionResponse is the result of "session/set_config_option".
type SetSessionConfigOptionResponse struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// ---------------------------------------------------------------------------
// Content model (ContentBlock union, discriminated on "type")
// ---------------------------------------------------------------------------

// ContentBlock is a discriminated union for prompt/content blocks.
// Discriminator: "type" field — "text", "image", "audio", "resource_link", "resource".
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "audio", "resource_link", "resource"
	Meta Meta   `json:"_meta,omitempty"`

	// Annotations (all content types).
	Annotations *Annotations `json:"annotations,omitempty"`

	// TextContent fields.
	Text string `json:"text,omitempty"`

	// ImageContent fields.
	MimeType string  `json:"mimeType,omitempty"`
	Data     string  `json:"data,omitempty"` // base64
	URI      *string `json:"uri,omitempty"`  // optional for image, required for resource_link

	// AudioContent fields (mimeType and data shared with image).

	// ResourceLink fields.
	Name        *string `json:"name,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Size        *int64  `json:"size,omitempty"`
	// URI and MimeType shared.

	// EmbeddedResource fields.
	Resource *EmbeddedResourceContents `json:"resource,omitempty"`
}

// EmbeddedResourceContents is a union of TextResourceContents and BlobResourceContents.
// If Text is non-empty, it's a text resource; if Blob is non-empty, it's a blob resource.
type EmbeddedResourceContents struct {
	Meta     Meta    `json:"_meta,omitempty"`
	URI      string  `json:"uri"`
	MimeType *string `json:"mimeType,omitempty"`
	// TextResourceContents.
	Text string `json:"text,omitempty"`
	// BlobResourceContents.
	Blob string `json:"blob,omitempty"`
}

// ContentWrapper wraps a ContentBlock with optional meta. Used in ToolCallContent.
type ContentWrapper struct {
	Meta    Meta         `json:"_meta,omitempty"`
	Content ContentBlock `json:"content"`
}

// ---------------------------------------------------------------------------
// Tool call types
// ---------------------------------------------------------------------------

// ToolCall represents a tool call in a session update.
type ToolCall struct {
	Meta       Meta               `json:"_meta,omitempty"`
	ToolCallID string             `json:"toolCallId"`
	Title      string             `json:"title"`
	Kind       string             `json:"kind,omitempty"`
	Status     string             `json:"status,omitempty"`
	RawInput   any                `json:"rawInput,omitempty"`
	RawOutput  any                `json:"rawOutput,omitempty"`
	Content    []ToolCallContent  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
}

// ToolCallUpdate is a partial update to a tool call.
type ToolCallUpdate struct {
	Meta       Meta               `json:"_meta,omitempty"`
	ToolCallID string             `json:"toolCallId"`
	Title      *string            `json:"title,omitempty"`
	Kind       *string            `json:"kind,omitempty"`
	Status     *string            `json:"status,omitempty"`
	RawInput   any                `json:"rawInput,omitempty"`
	RawOutput  any                `json:"rawOutput,omitempty"`
	Content    []ToolCallContent  `json:"content,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
}

// ToolCallContent is a discriminated union on "type": "content", "diff", "terminal".
type ToolCallContent struct {
	Type string `json:"type"` // "content", "diff", "terminal"
	Meta Meta   `json:"_meta,omitempty"`

	// Content variant.
	Content *ContentBlock `json:"content,omitempty"`

	// Diff variant.
	Path    string  `json:"path,omitempty"`
	OldText *string `json:"oldText,omitempty"`
	NewText string  `json:"newText,omitempty"`

	// Terminal variant.
	TerminalID string `json:"terminalId,omitempty"`
}

// ToolCallLocation is a file location associated with a tool call.
type ToolCallLocation struct {
	Meta Meta   `json:"_meta,omitempty"`
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// ---------------------------------------------------------------------------
// Plan types
// ---------------------------------------------------------------------------

// Plan represents a planning update.
type Plan struct {
	Meta    Meta        `json:"_meta,omitempty"`
	Entries []PlanEntry `json:"entries"`
}

// PlanEntry is a single plan item.
type PlanEntry struct {
	Meta     Meta   `json:"_meta,omitempty"`
	Content  string `json:"content"`
	Priority string `json:"priority"` // PlanEntryPriority
	Status   string `json:"status"`   // PlanEntryStatus
}

// ---------------------------------------------------------------------------
// Mode types
// ---------------------------------------------------------------------------

// SessionMode describes an available mode.
type SessionMode struct {
	Meta        Meta    `json:"_meta,omitempty"`
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// SessionModeState describes the current mode state.
type SessionModeState struct {
	Meta           Meta          `json:"_meta,omitempty"`
	AvailableModes []SessionMode `json:"availableModes"`
	CurrentModeID  string        `json:"currentModeId"`
}

// CurrentModeUpdate is a session update for mode changes.
type CurrentModeUpdate struct {
	Meta          Meta   `json:"_meta,omitempty"`
	CurrentModeID string `json:"currentModeId"`
}

// ---------------------------------------------------------------------------
// Model types (UNSTABLE)
// ---------------------------------------------------------------------------

// ModelInfo describes an available model.
type ModelInfo struct {
	Meta        Meta    `json:"_meta,omitempty"`
	ModelID     string  `json:"modelId"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// SessionModelState describes the current model state.
type SessionModelState struct {
	Meta            Meta        `json:"_meta,omitempty"`
	AvailableModels []ModelInfo `json:"availableModels"`
	CurrentModelID  string      `json:"currentModelId"`
}

// ---------------------------------------------------------------------------
// Config option types
// ---------------------------------------------------------------------------

// SessionConfigOption describes a configuration option (discriminated on "type").
// Currently only "select" variant exists.
type SessionConfigOption struct {
	Meta        Meta    `json:"_meta,omitempty"`
	Type        string  `json:"type"` // "select"
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Category    *string `json:"category,omitempty"` // SessionConfigOptionCategory
	Description *string `json:"description,omitempty"`

	// Select variant fields.
	CurrentValue string          `json:"currentValue,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"` // SessionConfigSelectOption[] or SessionConfigSelectGroup[]
}

// SessionConfigSelectOption is a single config select option.
type SessionConfigSelectOption struct {
	Meta        Meta    `json:"_meta,omitempty"`
	Value       string  `json:"value"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// SessionConfigSelectGroup is a group of config select options.
type SessionConfigSelectGroup struct {
	Meta    Meta                        `json:"_meta,omitempty"`
	Group   string                      `json:"group"`
	Name    string                      `json:"name"`
	Options []SessionConfigSelectOption `json:"options"`
}

// ConfigOptionUpdate is a session update for config changes.
type ConfigOptionUpdate struct {
	Meta          Meta                  `json:"_meta,omitempty"`
	ConfigOptions []SessionConfigOption `json:"configOptions"`
}

// ---------------------------------------------------------------------------
// Session info types (UNSTABLE)
// ---------------------------------------------------------------------------

// SessionInfoUpdate is a session update for session metadata changes.
type SessionInfoUpdate struct {
	Meta      Meta    `json:"_meta,omitempty"`
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`
}

// ---------------------------------------------------------------------------
// Usage types (UNSTABLE)
// ---------------------------------------------------------------------------

// Usage contains token usage information.
type Usage struct {
	InputTokens       int  `json:"inputTokens"`
	OutputTokens      int  `json:"outputTokens"`
	TotalTokens       int  `json:"totalTokens"`
	CachedReadTokens  *int `json:"cachedReadTokens,omitempty"`
	CachedWriteTokens *int `json:"cachedWriteTokens,omitempty"`
	ThoughtTokens     *int `json:"thoughtTokens,omitempty"`
}

// UsageUpdate is a session update for usage metrics.
type UsageUpdate struct {
	Meta Meta  `json:"_meta,omitempty"`
	Size int   `json:"size"`
	Used int   `json:"used"`
	Cost *Cost `json:"cost,omitempty"`
}

// Cost describes a monetary cost.
type Cost struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// ---------------------------------------------------------------------------
// Command types
// ---------------------------------------------------------------------------

// AvailableCommand describes a slash command.
type AvailableCommand struct {
	Meta        Meta                   `json:"_meta,omitempty"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Input       *AvailableCommandInput `json:"input,omitempty"`
}

// AvailableCommandInput describes input for a command.
type AvailableCommandInput struct {
	Meta Meta   `json:"_meta,omitempty"`
	Hint string `json:"hint"`
}

// AvailableCommandsUpdate is a session update for available commands.
type AvailableCommandsUpdate struct {
	Meta              Meta               `json:"_meta,omitempty"`
	AvailableCommands []AvailableCommand `json:"availableCommands"`
}

// ---------------------------------------------------------------------------
// Permission types
// ---------------------------------------------------------------------------

// PermissionOption is an option in a permission request.
type PermissionOption struct {
	Meta     Meta   `json:"_meta,omitempty"`
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // PermissionOptionKind
}

// RequestPermissionOutcome is a discriminated union on "outcome".
// Variant "cancelled": no additional fields.
// Variant "selected": includes optionId.
type RequestPermissionOutcome struct {
	Outcome  string `json:"outcome"`            // "cancelled" or "selected"
	OptionID string `json:"optionId,omitempty"` // only for "selected"
	Meta     Meta   `json:"_meta,omitempty"`
}

// ---------------------------------------------------------------------------
// Terminal exit status
// ---------------------------------------------------------------------------

// TerminalExitStatus describes how a terminal command exited.
type TerminalExitStatus struct {
	Meta     Meta    `json:"_meta,omitempty"`
	ExitCode *int    `json:"exitCode,omitempty"`
	Signal   *string `json:"signal,omitempty"`
}

// ---------------------------------------------------------------------------
// Prompt
// ---------------------------------------------------------------------------

// PromptRequest is the params for "session/prompt".
type PromptRequest struct {
	Meta      Meta           `json:"_meta,omitempty"`
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// PromptResponse is the result of "session/prompt".
type PromptResponse struct {
	Meta       Meta   `json:"_meta,omitempty"`
	StopReason string `json:"stopReason"` // StopReason
	Usage      *Usage `json:"usage,omitempty"`
}

// CancelNotification is the params for "session/cancel" (no response expected).
type CancelNotification struct {
	Meta      Meta   `json:"_meta,omitempty"`
	SessionID string `json:"sessionId"`
}

// CancelRequestNotification is the params for "$/cancel_request" (UNSTABLE).
type CancelRequestNotification struct {
	Meta      Meta `json:"_meta,omitempty"`
	RequestID any  `json:"requestId"` // null, number, or string
}

// ---------------------------------------------------------------------------
// Session update notifications (agent → client)
// ---------------------------------------------------------------------------

// SessionNotification is the params for "session/update" notifications.
type SessionNotification struct {
	Meta      Meta          `json:"_meta,omitempty"`
	SessionID string        `json:"sessionId"`
	Update    SessionUpdate `json:"update"`
}

// SessionUpdate is the update payload (discriminated on "sessionUpdate" field).
// Possible values: "user_message_chunk", "agent_message_chunk",
// "agent_thought_chunk", "tool_call", "tool_call_update", "plan",
// "available_commands_update", "current_mode_update", "config_option_update",
// "session_info_update", "usage_update".
type SessionUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`

	// ContentChunk variants: user_message_chunk, agent_message_chunk, agent_thought_chunk.
	Content *ContentBlock `json:"content,omitempty"`

	// tool_call variant.
	ToolCall *ToolCall `json:"toolCall,omitempty"`

	// tool_call_update variant.
	ToolCallUpdate *ToolCallUpdate `json:"toolCallUpdate,omitempty"`

	// plan variant.
	Plan *Plan `json:"plan,omitempty"`

	// available_commands_update variant.
	AvailableCommands []AvailableCommand `json:"availableCommands,omitempty"`

	// current_mode_update variant.
	CurrentModeID *string `json:"currentModeId,omitempty"`

	// config_option_update variant.
	ConfigOptions []SessionConfigOption `json:"configOptions,omitempty"`

	// session_info_update variant.
	Title     *string `json:"title,omitempty"`
	UpdatedAt *string `json:"updatedAt,omitempty"`

	// usage_update variant.
	Size *int  `json:"size,omitempty"`
	Used *int  `json:"used,omitempty"`
	Cost *Cost `json:"cost,omitempty"`

	// Meta for the update itself.
	Meta Meta `json:"_meta,omitempty"`
}

// ---------------------------------------------------------------------------
// Agent → Client requests
// ---------------------------------------------------------------------------

// RequestPermissionRequest asks the client for permission.
type RequestPermissionRequest struct {
	Meta      Meta               `json:"_meta,omitempty"`
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// RequestPermissionResponse is the client's permission decision.
type RequestPermissionResponse struct {
	Meta    Meta                     `json:"_meta,omitempty"`
	Outcome RequestPermissionOutcome `json:"outcome"`
}

// ReadTextFileRequest asks the client to read a file.
type ReadTextFileRequest struct {
	Meta      Meta   `json:"_meta,omitempty"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Line      *int   `json:"line,omitempty"`
	Limit     *int   `json:"limit,omitempty"`
}

// ReadTextFileResponse is the file content.
type ReadTextFileResponse struct {
	Meta    Meta   `json:"_meta,omitempty"`
	Content string `json:"content"`
}

// WriteTextFileRequest asks the client to write a file.
type WriteTextFileRequest struct {
	Meta      Meta   `json:"_meta,omitempty"`
	SessionID string `json:"sessionId"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

// WriteTextFileResponse confirms the write.
type WriteTextFileResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

// CreateTerminalRequest asks the client to create a terminal.
type CreateTerminalRequest struct {
	Meta            Meta          `json:"_meta,omitempty"`
	SessionID       string        `json:"sessionId"`
	Command         string        `json:"command"`
	Args            []string      `json:"args,omitempty"`
	CWD             *string       `json:"cwd,omitempty"`
	Env             []EnvVariable `json:"env,omitempty"`
	OutputByteLimit *int64        `json:"outputByteLimit,omitempty"`
}

// CreateTerminalResponse returns the terminal handle.
type CreateTerminalResponse struct {
	Meta       Meta   `json:"_meta,omitempty"`
	TerminalID string `json:"terminalId"`
}

// TerminalOutputRequest asks for terminal output.
type TerminalOutputRequest struct {
	Meta       Meta   `json:"_meta,omitempty"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalOutputResponse returns terminal output.
type TerminalOutputResponse struct {
	Meta       Meta                `json:"_meta,omitempty"`
	Output     string              `json:"output"`
	Truncated  bool                `json:"truncated"`
	ExitStatus *TerminalExitStatus `json:"exitStatus,omitempty"`
}

// TerminalReleaseRequest releases terminal resources.
type TerminalReleaseRequest struct {
	Meta       Meta   `json:"_meta,omitempty"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalReleaseResponse confirms the release.
type TerminalReleaseResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}

// TerminalWaitForExitRequest waits for terminal command completion.
type TerminalWaitForExitRequest struct {
	Meta       Meta   `json:"_meta,omitempty"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalWaitForExitResponse returns the exit status.
type TerminalWaitForExitResponse struct {
	Meta     Meta    `json:"_meta,omitempty"`
	ExitCode *int    `json:"exitCode,omitempty"`
	Signal   *string `json:"signal,omitempty"`
}

// TerminalKillRequest kills the terminal command.
type TerminalKillRequest struct {
	Meta       Meta   `json:"_meta,omitempty"`
	SessionID  string `json:"sessionId"`
	TerminalID string `json:"terminalId"`
}

// TerminalKillResponse confirms the kill.
type TerminalKillResponse struct {
	Meta Meta `json:"_meta,omitempty"`
}
