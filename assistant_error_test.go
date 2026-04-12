package claude

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestAssistantErrorUnmarshalString(t *testing.T) {
	var assistantErr AssistantError
	if err := json.Unmarshal([]byte("\"server_error\""), &assistantErr); err != nil {
		t.Fatalf("unmarshal string error: %v", err)
	}
	if assistantErr != AssistantErrorServerError {
		t.Fatalf("expected %q, got %q", AssistantErrorServerError, assistantErr)
	}
}

func TestAssistantErrorUnmarshalObjectFallback(t *testing.T) {
	var assistantErr AssistantError
	if err := json.Unmarshal([]byte(`{"type":"server_error","message":"boom"}`), &assistantErr); err != nil {
		t.Fatalf("unmarshal object error: %v", err)
	}
	if assistantErr != AssistantErrorServerError {
		t.Fatalf("expected %q, got %q", AssistantErrorServerError, assistantErr)
	}
}

func TestAssistantErrorKnownValues(t *testing.T) {
	tests := []struct {
		name  string
		value AssistantError
		raw   string
	}{
		{name: "authentication_failed", value: AssistantErrorAuthenticationFailed, raw: "authentication_failed"},
		{name: "billing_error", value: AssistantErrorBillingError, raw: "billing_error"},
		{name: "rate_limit", value: AssistantErrorRateLimit, raw: "rate_limit"},
		{name: "invalid_request", value: AssistantErrorInvalidRequest, raw: "invalid_request"},
		{name: "server_error", value: AssistantErrorServerError, raw: "server_error"},
		{name: "max_output_tokens", value: AssistantErrorMaxOutputTokens, raw: "max_output_tokens"},
		{name: "unknown", value: AssistantErrorUnknown, raw: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if string(test.value) != test.raw {
				t.Fatalf("expected constant %q, got %q", test.raw, test.value)
			}
			var assistantErr AssistantError
			if err := json.Unmarshal([]byte(strconv.Quote(test.raw)), &assistantErr); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if assistantErr != test.value {
				t.Fatalf("expected %q, got %q", test.value, assistantErr)
			}
		})
	}
}

func TestAssistantMessageNoError(t *testing.T) {
	var msg AssistantMessage
	payload := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		t.Fatalf("unmarshal assistant message: %v", err)
	}
	if msg.Error != nil {
		t.Fatalf("expected nil error, got %q", *msg.Error)
	}
}
