package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseIncomingMessageTypes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		kind messageKind
	}{
		{
			name: "system",
			raw:  `{"type":"system","subtype":"init"}`,
			kind: messageEvent,
		},
		{
			name: "assistant",
			raw:  `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
			kind: messageEvent,
		},
		{
			name: "user",
			raw:  `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}`,
			kind: messageEvent,
		},
		{
			name: "stream_event",
			raw:  `{"type":"stream_event","event":{"delta":"hi"}}`,
			kind: messageEvent,
		},
		{
			name: "rate_limit_event",
			raw:  `{"type":"rate_limit_event","event":{"remaining":42}}`,
			kind: messageEvent,
		},
		{
			name: "result",
			raw:  `{"type":"result","result":{"result":"done"},"is_error":false}`,
			kind: messageResult,
		},
		{
			name: "control_request",
			raw:  `{"type":"control_request","request_id":"req_1","request":{"subtype":"can_use_tool"}}`,
			kind: messageControlRequest,
		},
		{
			name: "control_response",
			raw:  `{"type":"control_response","response":{"subtype":"success","request_id":"req_1"}}`,
			kind: messageControlResponse,
		},
		{
			name: "control_cancel_request",
			raw:  `{"type":"control_cancel_request","request_id":"req_1"}`,
			kind: messageControlCancel,
		},
		{
			name: "unknown",
			raw:  `{"type":"future_message"}`,
			kind: messageUnknown,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseIncomingMessage(json.RawMessage(test.raw))
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if parsed.kind != test.kind {
				t.Fatalf("expected kind %v, got %v", test.kind, parsed.kind)
			}
			if test.kind == messageResult && parsed.result.Result.Text != "done" {
				t.Fatalf("expected result text 'done', got %q", parsed.result.Result.Text)
			}
		})
	}
}

func TestToolResultContentVariants(t *testing.T) {
	stringPayload := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok","is_error":false}]}}`
	parsed, err := parseIncomingMessage(json.RawMessage(stringPayload))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	user, ok := parsed.event.(UserMessage)
	if !ok {
		t.Fatalf("expected UserMessage, got %T", parsed.event)
	}
	if len(user.Message.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(user.Message.Content))
	}
	var text string
	if err := json.Unmarshal(user.Message.Content[0].Content, &text); err != nil {
		t.Fatalf("expected string content: %v", err)
	}
	if text != "ok" {
		t.Fatalf("expected content 'ok', got %q", text)
	}

	arrayPayload := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"ok"}],"is_error":false}]}}`
	parsed, err = parseIncomingMessage(json.RawMessage(arrayPayload))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	user, ok = parsed.event.(UserMessage)
	if !ok {
		t.Fatalf("expected UserMessage, got %T", parsed.event)
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(user.Message.Content[0].Content, &blocks); err != nil {
		t.Fatalf("expected array content: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Text != "ok" {
		t.Fatalf("unexpected content blocks: %+v", blocks)
	}
}

func TestPermissionDenialsVariants(t *testing.T) {
	var denials PermissionDenials
	if err := json.Unmarshal([]byte(`""`), &denials); err == nil {
		t.Fatalf("expected error for invalid payload")
	}
	if err := json.Unmarshal([]byte(`["denied"]`), &denials); err != nil {
		t.Fatalf("string list unmarshal failed: %v", err)
	}
	if len(denials) != 1 || denials[0].Message != "denied" {
		t.Fatalf("unexpected denials: %+v", denials)
	}
	if err := json.Unmarshal([]byte(`[{"tool_name":"bash","reason":"blocked"}]`), &denials); err != nil {
		t.Fatalf("object list unmarshal failed: %v", err)
	}
	if len(denials) != 1 || denials[0].ToolName != "bash" {
		t.Fatalf("unexpected denials: %+v", denials)
	}
}

func TestTransportReadWrite(t *testing.T) {
	stdout := io.NopCloser(strings.NewReader("[SandboxDebug] hi\n{\"type\":\"system\",\"subtype\":\"init\"}\nnotjson\n{\"type\":\"result\",\"result\":{\"result\":\"ok\"},\"is_error\":false}\n"))
	stdin := &bufferWriteCloser{}
	transport := newTransport(stdin, stdout)
	readCh, errCh := transport.StartRead(context.Background())
	var messages []string
	for msg := range readCh {
		messages = append(messages, string(msg))
	}
	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected scanner error: %v", err)
		}
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if !strings.Contains(messages[0], "\"type\":\"system\"") {
		t.Fatalf("unexpected first message: %s", messages[0])
	}
	if err := transport.WriteJSON(struct {
		Type string `json:"type"`
	}{Type: "ping"}); err != nil {
		t.Fatalf("write json failed: %v", err)
	}
	if got := stdin.String(); got != "{\"type\":\"ping\"}\n" {
		t.Fatalf("unexpected write output: %q", got)
	}
}

func TestCloseForcesKill(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	oldTimeout := shutdownWaitTimeout
	shutdownWaitTimeout = 1 * time.Second
	defer func() { shutdownWaitTimeout = oldTimeout }()
	cmd := exec.Command("sh", "-c", "trap '' TERM; while true; do sleep 1; done")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	transport := newTransport(stdin, stdout)
	readCh, errCh := transport.StartRead(context.Background())
	client := &Client{
		cmd:       cmd,
		transport: transport,
		readCh:    readCh,
		errCh:     errCh,
		waitCh:    make(chan struct{}),
		closed:    make(chan struct{}),
	}
	go func() {
		waitErr := cmd.Wait()
		client.setWaitErr(waitErr)
		close(client.waitCh)
	}()
	if err := client.Close(); err == nil {
		t.Fatalf("expected close error")
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected exit error, got %T", err)
		}
		status, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok {
			t.Fatalf("unexpected wait status: %T", exitErr.Sys())
		}
		if status.Signal() != syscall.SIGKILL {
			t.Fatalf("expected SIGKILL, got %v", status.Signal())
		}
	}
}

type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error {
	return nil
}
