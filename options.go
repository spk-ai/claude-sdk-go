package claude

import "io"

const defaultBinaryPath = "claude"

// Options configures the Claude Code subprocess.
type Options struct {
	BinaryPath   string
	WorkDir      string
	Env          []string
	SystemPrompt string
	Model        string
	MaxTurns     int
	Stderr       io.Writer
	// SessionID assigns a UUID to a new session. Mutually exclusive with Resume.
	SessionID string
	// Resume selects an existing session by ID, name, or transcript path. The
	// CLI must have access to that session's persisted state. No fallback to a
	// new session is attempted by the SDK.
	Resume string
}
