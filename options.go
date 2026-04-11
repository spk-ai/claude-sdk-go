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
}
