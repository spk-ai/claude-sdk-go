package claude

import "io"

const defaultBinaryPath = "claude"

// Options configures the Claude Code subprocess.
// Empty SessionID and Resume leave the CLI's default session selection unchanged.
// These options do not persist or copy native session files.
type Options struct {
	BinaryPath   string
	WorkDir      string
	Env          []string
	SystemPrompt string
	Model        string
	MaxTurns     int
	Stderr       io.Writer
	// SessionID forwards a new session UUID as --session-id. The CLI validates
	// the UUID; Start only enforces mutual exclusion with Resume.
	SessionID string
	// Resume forwards an existing session ID, name, or transcript path as
	// --resume. The CLI needs access to the persisted state; the SDK does not
	// fall back to a new session. Mutually exclusive with SessionID.
	Resume string
}
