package claude

import (
	"errors"
	"fmt"
)

var (
	// ErrClientClosed indicates the client is no longer usable.
	ErrClientClosed = errors.New("claude client is closed")
)

// ProtocolError indicates a protocol-level issue with Claude Code.
type ProtocolError struct {
	Message string
}

func (err ProtocolError) Error() string {
	return err.Message
}

// ProcessExitError wraps a subprocess exit error.
type ProcessExitError struct {
	Err error
}

func (err ProcessExitError) Error() string {
	return fmt.Sprintf("claude process exited: %v", err.Err)
}

func (err ProcessExitError) Unwrap() error {
	return err.Err
}
