package claude

const defaultBinaryPath = "claude"

// Options configures the Claude Code subprocess.
type Options struct {
	BinaryPath   string
	WorkDir      string
	Env          []string
	SystemPrompt string
	Model        string
	MaxTurns     int
}

// Option applies configuration to Options.
type Option func(*Options)

// DefaultOptions returns Options with default values applied.
func DefaultOptions() Options {
	return Options{BinaryPath: defaultBinaryPath}
}

// NewOptions builds Options using functional options.
func NewOptions(opts ...Option) Options {
	return ApplyOptions(DefaultOptions(), opts...)
}

// ApplyOptions applies functional options to a base Options value.
func ApplyOptions(base Options, opts ...Option) Options {
	options := base
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	return options
}

// WithBinary sets the Claude Code binary path.
func WithBinary(path string) Option {
	return func(opts *Options) {
		opts.BinaryPath = path
	}
}

// WithWorkDir sets the subprocess working directory.
func WithWorkDir(dir string) Option {
	return func(opts *Options) {
		opts.WorkDir = dir
	}
}

// WithEnv sets additional environment variables.
func WithEnv(env []string) Option {
	return func(opts *Options) {
		opts.Env = append([]string(nil), env...)
	}
}

// WithSystemPrompt sets the system prompt for initialization.
func WithSystemPrompt(prompt string) Option {
	return func(opts *Options) {
		opts.SystemPrompt = prompt
	}
}

// WithModel sets the model override for initialization.
func WithModel(model string) Option {
	return func(opts *Options) {
		opts.Model = model
	}
}

// WithMaxTurns sets the maximum turns limit.
func WithMaxTurns(max int) Option {
	return func(opts *Options) {
		opts.MaxTurns = max
	}
}
