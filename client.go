package claude

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	shutdownWaitTimeout = 5 * time.Second
	errExitTimeout      = errors.New("timeout waiting for process to exit")
)

// Client manages a Claude Code subprocess.
type Client struct {
	cmd       *exec.Cmd
	transport *transport
	readCh    <-chan json.RawMessage
	errCh     <-chan error
	waitCh    chan struct{}
	waitErr   error
	waitMu    sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}

	pendingEvents []Event
	turnMu        sync.Mutex
}

// TurnParams configures a single conversation turn.
type TurnParams struct {
	Prompt string
}

// TurnResult contains the outcome of a completed turn.
type TurnResult struct {
	SessionID  string
	Response   string
	IsError    bool
	DurationMs int
	NumTurns   int
	Usage      *Usage
	StopReason string
}

// EventHandler receives streaming events during a turn.
type EventHandler func(event Event)

// Start spawns the Claude Code subprocess and performs the initialize handshake.
func Start(ctx context.Context, opts Options) (*Client, error) {
	if opts.SessionID != "" && opts.Resume != "" {
		return nil, fmt.Errorf("SessionID and Resume are mutually exclusive")
	}
	if opts.BinaryPath == "" {
		opts.BinaryPath = defaultBinaryPath
	}
	args := []string{"--output-format", "stream-json", "--input-format", "stream-json", "--verbose"}
	if opts.SessionID != "" {
		args = append(args, "--session-id="+opts.SessionID)
	}
	if opts.Resume != "" {
		args = append(args, "--resume="+opts.Resume)
	}
	cmd := exec.Command(opts.BinaryPath, args...)
	cmd.Env = buildEnv(opts.Env)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	transport := newTransport(stdin, stdout)
	readCh, errCh := transport.StartRead(ctx)
	client := &Client{
		cmd:       cmd,
		transport: transport,
		readCh:    readCh,
		errCh:     errCh,
		waitCh:    make(chan struct{}),
		closed:    make(chan struct{}),
	}
	go func() {
		err := cmd.Wait()
		client.setWaitErr(err)
		close(client.waitCh)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-client.closed:
			return
		}
	}()
	if err := client.initialize(ctx, opts); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Turn sends a user message and blocks until the turn completes.
func (c *Client) Turn(ctx context.Context, params TurnParams, handler EventHandler) (*TurnResult, error) {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.isClosed() {
		return nil, ErrClientClosed
	}
	if handler == nil {
		handler = func(Event) {}
	}
	for _, event := range c.pendingEvents {
		handler(event)
	}
	c.pendingEvents = nil
	userMessage := outboundUserMessage{
		Type:      "user",
		SessionID: "",
		Message: outboundMessagePayload{
			Role:    "user",
			Content: params.Prompt,
		},
		ParentToolUseID: nil,
	}
	if err := c.transport.WriteJSON(userMessage); err != nil {
		return nil, err
	}
	for {
		raw, err := c.readMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = c.Close()
			}
			return nil, err
		}
		parsed, err := parseIncomingMessage(raw)
		if err != nil {
			return nil, err
		}
		switch parsed.kind {
		case messageControlRequest:
			if err := c.handleControlRequest(parsed.controlRequest); err != nil {
				return nil, err
			}
		case messageEvent:
			handler(parsed.event)
		case messageResult:
			return parsed.result.toTurnResult(), nil
		default:
			continue
		}
	}
}

// Close gracefully shuts down the subprocess.
func (c *Client) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.transport.CloseWriter()
		if err := c.waitForExit(shutdownWaitTimeout); err != nil {
			if !errors.Is(err, errExitTimeout) {
				closeErr = ProcessExitError{Err: err}
				return
			}
			_ = c.signal(syscall.SIGTERM)
			if err := c.waitForExit(shutdownWaitTimeout); err != nil {
				if !errors.Is(err, errExitTimeout) {
					closeErr = ProcessExitError{Err: err}
					return
				}
				_ = c.signal(syscall.SIGKILL)
				closeErr = c.waitForExit(shutdownWaitTimeout)
				if closeErr != nil {
					closeErr = ProcessExitError{Err: closeErr}
				}
			}
		}
	})
	return closeErr
}

type outboundUserMessage struct {
	Type            string                 `json:"type"`
	SessionID       string                 `json:"session_id"`
	Message         outboundMessagePayload `json:"message"`
	ParentToolUseID *string                `json:"parent_tool_use_id"`
}

type outboundMessagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (c *Client) initialize(ctx context.Context, opts Options) error {
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	var maxTurns *int
	if opts.MaxTurns > 0 {
		maxTurns = &opts.MaxTurns
	}
	request := InitializeControlRequest{
		Type:      "control_request",
		RequestID: requestID,
		Request: InitializeRequest{
			Subtype:      "initialize",
			SystemPrompt: opts.SystemPrompt,
			Model:        opts.Model,
			MaxTurns:     maxTurns,
		},
	}
	if err := c.transport.WriteJSON(request); err != nil {
		return err
	}
	for {
		raw, err := c.readMessage(ctx)
		if err != nil {
			return err
		}
		parsed, err := parseIncomingMessage(raw)
		if err != nil {
			return err
		}
		switch parsed.kind {
		case messageControlResponse:
			if parsed.controlResponse.Response.RequestID != requestID {
				continue
			}
			if parsed.controlResponse.Response.Subtype != "success" {
				return ProtocolError{Message: fmt.Sprintf("initialize failed: %s", parsed.controlResponse.Response.Subtype)}
			}
			return nil
		case messageControlRequest:
			if err := c.handleControlRequest(parsed.controlRequest); err != nil {
				return err
			}
		case messageEvent:
			c.pendingEvents = append(c.pendingEvents, parsed.event)
		default:
			continue
		}
	}
}

func (c *Client) handleControlRequest(request *ControlRequestMessage) error {
	if request.Request.Subtype != "can_use_tool" {
		return nil
	}
	response := ControlResponse{
		Type: "control_response",
		Response: ControlResponseResult{
			Subtype:   "success",
			RequestID: request.RequestID,
			Response: &ToolPermissionResponse{
				Behavior:           "allow",
				UpdatedInput:       nil,
				UpdatedPermissions: []string{},
			},
		},
	}
	return c.transport.WriteJSON(response)
}

func (c *Client) readMessage(ctx context.Context) (json.RawMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.waitCh:
			return nil, c.processExitError()
		case err, ok := <-c.errCh:
			if ok && err != nil {
				return nil, err
			}
			if !ok {
				c.errCh = nil
			}
		case raw, ok := <-c.readCh:
			if !ok {
				return nil, c.processExitError()
			}
			return raw, nil
		}
	}
}

func (c *Client) waitForExit(timeout time.Duration) error {
	select {
	case <-c.waitCh:
		return c.getWaitErr()
	case <-time.After(timeout):
		return errExitTimeout
	}
}

func (c *Client) signal(sig syscall.Signal) error {
	if c.cmd.Process == nil {
		return nil
	}
	return c.cmd.Process.Signal(sig)
}

func (c *Client) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *Client) processExitError() error {
	if err := c.getWaitErr(); err != nil {
		return ProcessExitError{Err: err}
	}
	return ErrClientClosed
}

func (c *Client) setWaitErr(err error) {
	c.waitMu.Lock()
	defer c.waitMu.Unlock()
	c.waitErr = err
}

func (c *Client) getWaitErr() error {
	c.waitMu.Lock()
	defer c.waitMu.Unlock()
	return c.waitErr
}

func newRequestID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func buildEnv(extra []string) []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+len(extra))
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDECODE=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	filtered = append(filtered, extra...)
	return filtered
}

type messageKind int

const (
	messageUnknown messageKind = iota
	messageEvent
	messageResult
	messageControlRequest
	messageControlResponse
	messageControlCancel
)

type parsedMessage struct {
	kind            messageKind
	event           Event
	result          *ResultMessage
	controlRequest  *ControlRequestMessage
	controlResponse *ControlResponseMessage
	controlCancel   *ControlCancelRequestMessage
}

func parseIncomingMessage(raw json.RawMessage) (parsedMessage, error) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return parsedMessage{}, err
	}
	switch envelope.Type {
	case "system":
		var msg SystemMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageEvent, event: msg}, nil
	case "assistant":
		var msg AssistantMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageEvent, event: msg}, nil
	case "user":
		var msg UserMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageEvent, event: msg}, nil
	case "stream_event":
		var msg StreamEventMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageEvent, event: msg}, nil
	case "rate_limit_event":
		var msg RateLimitEventMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageEvent, event: msg}, nil
	case "result":
		var msg ResultMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageResult, result: &msg}, nil
	case "control_request":
		var msg ControlRequestMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageControlRequest, controlRequest: &msg}, nil
	case "control_response":
		var msg ControlResponseMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageControlResponse, controlResponse: &msg}, nil
	case "control_cancel_request":
		var msg ControlCancelRequestMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return parsedMessage{}, err
		}
		return parsedMessage{kind: messageControlCancel, controlCancel: &msg}, nil
	default:
		return parsedMessage{kind: messageUnknown}, nil
	}
}

func (result *ResultMessage) toTurnResult() *TurnResult {
	if result == nil {
		return nil
	}
	return &TurnResult{
		SessionID:  result.SessionID,
		Response:   result.Result.Text,
		IsError:    result.IsError,
		DurationMs: result.DurationMs,
		NumTurns:   result.NumTurns,
		Usage:      result.Usage,
		StopReason: result.StopReason,
	}
}
