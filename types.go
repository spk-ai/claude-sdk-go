package claude

import (
	"encoding/json"
	"errors"
)

// Event represents a streaming event emitted during a turn.
type Event interface {
	EventType() string
}

// SystemMessage represents lifecycle/system output from Claude Code.
type SystemMessage struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	SessionID string          `json:"session_id,omitempty"`
	Message   string          `json:"message,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func (SystemMessage) EventType() string {
	return "system"
}

// AssistantMessage represents assistant output with content blocks.
type AssistantMessage struct {
	Type      string          `json:"type"`
	Message   ChatMessage     `json:"message"`
	SessionID string          `json:"session_id,omitempty"`
	Error     *AssistantError `json:"error,omitempty"`
}

func (AssistantMessage) EventType() string {
	return "assistant"
}

// AssistantError represents an API-level error from Claude.
type AssistantError string

const (
	AssistantErrorAuthenticationFailed AssistantError = "authentication_failed"
	AssistantErrorBillingError         AssistantError = "billing_error"
	AssistantErrorRateLimit            AssistantError = "rate_limit"
	AssistantErrorInvalidRequest       AssistantError = "invalid_request"
	AssistantErrorServerError          AssistantError = "server_error"
	AssistantErrorMaxOutputTokens      AssistantError = "max_output_tokens"
	AssistantErrorUnknown              AssistantError = "unknown"
)

func (assistantErr *AssistantError) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*assistantErr = AssistantError(asString)
		return nil
	}
	var payload struct {
		Type    string `json:"type"`
		Message string `json:"message,omitempty"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.Type == "" {
		return errors.New("assistant error type missing")
	}
	*assistantErr = AssistantError(payload.Type)
	return nil
}

// UserMessage represents tool result messages emitted by Claude Code.
type UserMessage struct {
	Type      string      `json:"type"`
	Message   ChatMessage `json:"message"`
	SessionID string      `json:"session_id,omitempty"`
}

func (UserMessage) EventType() string {
	return "user"
}

// StreamEventMessage represents partial streaming events.
type StreamEventMessage struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event,omitempty"`
}

func (StreamEventMessage) EventType() string {
	return "stream_event"
}

// RateLimitEventMessage represents rate limit updates.
type RateLimitEventMessage struct {
	Type  string          `json:"type"`
	Event json.RawMessage `json:"event,omitempty"`
}

func (RateLimitEventMessage) EventType() string {
	return "rate_limit_event"
}

// ChatMessage represents a role/content payload.
type ChatMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a single content block in a message.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ResultMessage signals turn completion.
type ResultMessage struct {
	Type              string            `json:"type"`
	SessionID         string            `json:"session_id,omitempty"`
	Result            ResultData        `json:"result"`
	IsError           bool              `json:"is_error"`
	Subtype           string            `json:"subtype,omitempty"`
	DurationMs        int               `json:"duration_ms,omitempty"`
	NumTurns          int               `json:"num_turns,omitempty"`
	Usage             *Usage            `json:"usage,omitempty"`
	ModelUsage        *ModelUsage       `json:"modelUsage,omitempty"`
	StopReason        string            `json:"stop_reason,omitempty"`
	APIErrorStatus    *int              `json:"api_error_status,omitempty"`
	TerminalReason    string            `json:"terminal_reason,omitempty"`
	PermissionDenials PermissionDenials `json:"permission_denials,omitempty"`
}

// ResultData captures the final response text.
type ResultData struct {
	Text string
	Raw  json.RawMessage
}

func (result *ResultData) UnmarshalJSON(data []byte) error {
	result.Raw = append(result.Raw[:0], data...)
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		result.Text = text
		return nil
	}
	var payload struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	result.Text = payload.Result
	return nil
}

// Usage uses snake_case fields in JSON.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

// ModelUsage uses camelCase fields in JSON.
type ModelUsage struct {
	InputTokens              int `json:"inputTokens"`
	OutputTokens             int `json:"outputTokens"`
	CacheReadInputTokens     int `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens int `json:"cacheCreationInputTokens,omitempty"`
}

// PermissionDenial represents a tool permission denial.
type PermissionDenial struct {
	ToolName string `json:"tool_name,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
}

// PermissionDenials handles string or object lists.
type PermissionDenials []PermissionDenial

func (denials *PermissionDenials) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*denials = nil
		return nil
	}
	var asStrings []string
	if err := json.Unmarshal(data, &asStrings); err == nil {
		items := make([]PermissionDenial, len(asStrings))
		for i, value := range asStrings {
			items[i] = PermissionDenial{Message: value}
		}
		*denials = items
		return nil
	}
	var asObjects []PermissionDenial
	if err := json.Unmarshal(data, &asObjects); err != nil {
		return err
	}
	*denials = asObjects
	return nil
}

// ControlRequestMessage represents control requests from Claude Code.
type ControlRequestMessage struct {
	Type      string                `json:"type"`
	RequestID string                `json:"request_id"`
	Request   ControlRequestPayload `json:"request"`
}

// ControlRequestPayload is the payload for a control request.
type ControlRequestPayload struct {
	Subtype           string            `json:"subtype"`
	PermissionDenials PermissionDenials `json:"permission_denials,omitempty"`
}

// ControlResponseMessage represents control responses from Claude Code.
type ControlResponseMessage struct {
	Type     string                 `json:"type"`
	Response ControlResponsePayload `json:"response"`
}

// ControlResponsePayload is the payload for a control response.
type ControlResponsePayload struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response,omitempty"`
	Error     *ControlError   `json:"error,omitempty"`
}

// ControlError represents errors in control responses.
type ControlError struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// ControlCancelRequestMessage represents canceled control requests.
type ControlCancelRequestMessage struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Reason    string `json:"reason,omitempty"`
}

// InitializeControlRequest sends the initialize handshake.
type InitializeControlRequest struct {
	Type      string            `json:"type"`
	RequestID string            `json:"request_id"`
	Request   InitializeRequest `json:"request"`
}

// InitializeRequest carries initialization options.
type InitializeRequest struct {
	Subtype      string `json:"subtype"`
	SystemPrompt string `json:"systemPrompt,omitempty"`
	Model        string `json:"model,omitempty"`
	MaxTurns     *int   `json:"maxTurns,omitempty"`
}

// ControlResponse sends a response to a control request.
type ControlResponse struct {
	Type     string                `json:"type"`
	Response ControlResponseResult `json:"response"`
}

// ControlResponseResult is sent by the SDK to the CLI.
type ControlResponseResult struct {
	Subtype   string                  `json:"subtype"`
	RequestID string                  `json:"request_id"`
	Response  *ToolPermissionResponse `json:"response,omitempty"`
}

// ToolPermissionResponse approves or updates tool permissions.
type ToolPermissionResponse struct {
	Behavior           string           `json:"behavior"`
	UpdatedInput       *json.RawMessage `json:"updatedInput"`
	UpdatedPermissions []string         `json:"updatedPermissions"`
}
