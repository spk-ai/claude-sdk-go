package claude

import (
	"bufio"
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

func TestParseIncomingMessageInvalidJSON(t *testing.T) {
	_, err := parseIncomingMessage(json.RawMessage(`{"type":"system"`))
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

func TestResultDataUnmarshalObject(t *testing.T) {
	var result ResultData
	if err := json.Unmarshal([]byte(`{"result":"finished"}`), &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if result.Text != "finished" {
		t.Fatalf("expected text 'finished', got %q", result.Text)
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

func TestBuildEnv(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("KEEP_ME", "yes")
	env := buildEnv([]string{"EXTRA_ENV=1"})
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDECODE=") {
			t.Fatalf("CLAUDECODE should be stripped")
		}
	}
	if !containsEnv(env, "KEEP_ME=yes") {
		t.Fatalf("expected KEEP_ME to be preserved")
	}
	if !containsEnv(env, "EXTRA_ENV=1") {
		t.Fatalf("expected EXTRA_ENV to be appended")
	}
}

func TestInitializeHandshake(t *testing.T) {
	client, scanner, writer, cleanup := newMockPipeClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opts := Options{SystemPrompt: "system", Model: "model", MaxTurns: 2}
	serverErr := make(chan error, 1)
	go func() {
		raw, err := readJSONLine(scanner)
		if err != nil {
			serverErr <- err
			return
		}
		var req InitializeControlRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			serverErr <- err
			return
		}
		if req.Request.Subtype != "initialize" {
			serverErr <- errors.New("expected initialize request")
			return
		}
		if req.Request.SystemPrompt != opts.SystemPrompt {
			serverErr <- errors.New("system prompt mismatch")
			return
		}
		if req.Request.Model != opts.Model {
			serverErr <- errors.New("model mismatch")
			return
		}
		if req.Request.MaxTurns == nil || *req.Request.MaxTurns != opts.MaxTurns {
			serverErr <- errors.New("max turns mismatch")
			return
		}
		resp := ControlResponseMessage{
			Type: "control_response",
			Response: ControlResponsePayload{
				Subtype:   "success",
				RequestID: req.RequestID,
			},
		}
		if err := writeJSONLine(writer, resp); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	if err := client.initialize(ctx, opts); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestTurnEndToEnd(t *testing.T) {
	client, scanner, writer, cleanup := newMockPipeClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() {
		initRaw, err := readJSONLine(scanner)
		if err != nil {
			serverErr <- err
			return
		}
		var initReq InitializeControlRequest
		if err := json.Unmarshal(initRaw, &initReq); err != nil {
			serverErr <- err
			return
		}
		initResp := ControlResponseMessage{
			Type: "control_response",
			Response: ControlResponsePayload{
				Subtype:   "success",
				RequestID: initReq.RequestID,
			},
		}
		if err := writeJSONLine(writer, initResp); err != nil {
			serverErr <- err
			return
		}

		userRaw, err := readJSONLine(scanner)
		if err != nil {
			serverErr <- err
			return
		}
		var user outboundUserMessage
		if err := json.Unmarshal(userRaw, &user); err != nil {
			serverErr <- err
			return
		}
		if user.Type != "user" || user.Message.Content != "Hello" {
			serverErr <- errors.New("unexpected user message")
			return
		}
		if user.ParentToolUseID != nil {
			serverErr <- errors.New("parent_tool_use_id should be null")
			return
		}
		if err := writeJSONLine(writer, SystemMessage{Type: "system", Subtype: "init"}); err != nil {
			serverErr <- err
			return
		}
		assistant := AssistantMessage{
			Type: "assistant",
			Message: ChatMessage{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "text", Text: "hi"},
				},
			},
		}
		if err := writeJSONLine(writer, assistant); err != nil {
			serverErr <- err
			return
		}
		requestID := "req-tool"
		controlReq := ControlRequestMessage{
			Type:      "control_request",
			RequestID: requestID,
			Request: ControlRequestPayload{
				Subtype: "can_use_tool",
			},
		}
		if err := writeJSONLine(writer, controlReq); err != nil {
			serverErr <- err
			return
		}
		controlRaw, err := readJSONLine(scanner)
		if err != nil {
			serverErr <- err
			return
		}
		var controlResp ControlResponse
		if err := json.Unmarshal(controlRaw, &controlResp); err != nil {
			serverErr <- err
			return
		}
		if controlResp.Response.RequestID != requestID || controlResp.Response.Subtype != "success" {
			serverErr <- errors.New("unexpected control response")
			return
		}
		if controlResp.Response.Response == nil || controlResp.Response.Response.Behavior != "allow" {
			serverErr <- errors.New("expected allow response")
			return
		}
		resultPayload := map[string]any{
			"type":        "result",
			"session_id":  "session-1",
			"result":      map[string]any{"result": "final"},
			"is_error":    false,
			"duration_ms": 123,
			"num_turns":   1,
			"stop_reason": "end",
			"usage": map[string]any{
				"input_tokens":  1,
				"output_tokens": 2,
			},
		}
		if err := writeJSONLine(writer, resultPayload); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	if err := client.initialize(ctx, Options{}); err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	var events []Event
	result, err := client.Turn(ctx, TurnParams{Prompt: "Hello"}, func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if _, ok := events[0].(SystemMessage); !ok {
		t.Fatalf("expected system message, got %T", events[0])
	}
	if _, ok := events[1].(AssistantMessage); !ok {
		t.Fatalf("expected assistant message, got %T", events[1])
	}
	if result.Response != "final" {
		t.Fatalf("expected response 'final', got %q", result.Response)
	}
	if result.DurationMs != 123 || result.NumTurns != 1 {
		t.Fatalf("unexpected duration or turns: %+v", result)
	}
	if result.StopReason != "end" {
		t.Fatalf("unexpected stop reason: %q", result.StopReason)
	}
	if result.Usage == nil || result.Usage.InputTokens != 1 || result.Usage.OutputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestAssistantErrorUnmarshalString(t *testing.T) {
	raw := `{"type":"assistant","message":{"role":"assistant","content":[]},"error":"server_error","session_id":"abc"}`
	parsed, err := parseIncomingMessage(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	msg, ok := parsed.event.(AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", parsed.event)
	}
	if msg.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if *msg.Error != AssistantErrorServerError {
		t.Fatalf("expected server_error, got %q", *msg.Error)
	}
}

func TestAssistantErrorUnmarshalObject(t *testing.T) {
	raw := `{"type":"assistant","message":{"role":"assistant","content":[]},"error":{"type":"rate_limit"},"session_id":"abc"}`
	parsed, err := parseIncomingMessage(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	msg, ok := parsed.event.(AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", parsed.event)
	}
	if msg.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if *msg.Error != AssistantErrorRateLimit {
		t.Fatalf("expected rate_limit, got %q", *msg.Error)
	}
}

func TestAssistantErrorAllKnownValues(t *testing.T) {
	values := []AssistantError{
		AssistantErrorAuthenticationFailed,
		AssistantErrorBillingError,
		AssistantErrorRateLimit,
		AssistantErrorInvalidRequest,
		AssistantErrorServerError,
		AssistantErrorMaxOutputTokens,
		AssistantErrorUnknown,
	}
	for _, val := range values {
		data, err := json.Marshal(val)
		if err != nil {
			t.Fatalf("marshal %q: %v", val, err)
		}
		var got AssistantError
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %q: %v", val, err)
		}
		if got != val {
			t.Fatalf("roundtrip: expected %q, got %q", val, got)
		}
	}
}

func TestAssistantMessageNoError(t *testing.T) {
	raw := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`
	parsed, err := parseIncomingMessage(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	msg, ok := parsed.event.(AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", parsed.event)
	}
	if msg.Error != nil {
		t.Fatalf("expected nil error, got %q", *msg.Error)
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

func newMockPipeClient(t *testing.T) (*Client, *bufio.Scanner, *bufio.Writer, func()) {
	t.Helper()
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	transport := newTransport(stdinWriter, stdoutReader)
	readCh, errCh := transport.StartRead(ctx)
	client := &Client{
		transport: transport,
		readCh:    readCh,
		errCh:     errCh,
		waitCh:    make(chan struct{}),
		closed:    make(chan struct{}),
	}
	scanner := bufio.NewScanner(stdinReader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScannerBuffer)
	writer := bufio.NewWriter(stdoutWriter)
	cleanup := func() {
		cancel()
		_ = stdoutWriter.Close()
		_ = stdinReader.Close()
	}
	return client, scanner, writer, cleanup
}

func readJSONLine(scanner *bufio.Scanner) (json.RawMessage, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return json.RawMessage(append([]byte(nil), scanner.Bytes()...)), nil
}

func writeJSONLine(writer *bufio.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := writer.Write(data); err != nil {
		return err
	}
	return writer.Flush()
}

func containsEnv(env []string, target string) bool {
	for _, entry := range env {
		if entry == target {
			return true
		}
	}
	return false
}

type bufferWriteCloser struct {
	bytes.Buffer
}

func (b *bufferWriteCloser) Close() error {
	return nil
}
