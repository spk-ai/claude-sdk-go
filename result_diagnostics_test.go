package claude

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestTurnResultDiagnostics(t *testing.T) {
	status := 401
	for _, tt := range []struct {
		name            string
		fields          map[string]any
		isError         bool
		subtype, reason string
		status          *int
	}{
		{name: "legacy", fields: map[string]any{}},
		{name: "explicit null", fields: map[string]any{"api_error_status": nil}},
		{name: "completed", fields: map[string]any{"subtype": "success", "terminal_reason": "completed"}, subtype: "success", reason: "completed"},
		{name: "API failure with success subtype", fields: map[string]any{"is_error": true, "subtype": "success", "terminal_reason": "api_error", "api_error_status": 401}, isError: true, subtype: "success", reason: "api_error", status: &status},
		{name: "execution failure", fields: map[string]any{"is_error": true, "subtype": "error_during_execution"}, isError: true, subtype: "error_during_execution"},
		{name: "future values preserved", fields: map[string]any{"is_error": true, "subtype": "future_subtype", "terminal_reason": "future_reason"}, isError: true, subtype: "future_subtype", reason: "future_reason"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, scanner, writer, cleanup := newMockPipeClient(t)
			defer cleanup()
			payload := map[string]any{"type": "result", "session_id": "session", "result": "response"}
			for key, value := range tt.fields {
				payload[key] = value
			}
			serverErr := make(chan error, 1)
			go func() {
				_, err := readJSONLine(scanner)
				if err == nil {
					err = writeJSONLine(writer, payload)
				}
				serverErr <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := client.Turn(ctx, TurnParams{Prompt: "work"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
			if result.IsError != tt.isError || result.Subtype != tt.subtype || result.TerminalReason != tt.reason || !reflect.DeepEqual(result.APIErrorStatus, tt.status) {
				t.Fatalf("unexpected result diagnostics: %+v", result)
			}
			if result.SessionID != "session" || result.Response != "response" {
				t.Fatal("existing result fields changed")
			}
		})
	}
}

func TestResultDiagnosticsRejectMalformedStatus(t *testing.T) {
	for _, raw := range []string{
		`{"type":"result","is_error":true,"api_error_status":"401"}`,
		`{"type":"result","is_error":true,"api_error_status":401.5}`,
		`{"type":"result","is_error":true,"api_error_status":{}}`,
	} {
		if _, err := parseIncomingMessage([]byte(raw)); err == nil {
			t.Fatalf("accepted malformed status: %s", raw)
		}
	}
}
