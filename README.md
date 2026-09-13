# Claude Code Go SDK

This repository provides a Go SDK for running the Claude Code CLI as a
subprocess and communicating over the NDJSON (newline-delimited JSON) stream
protocol.

## Overview

- Spawns the Claude Code binary with:
  `--output-format stream-json --input-format stream-json --verbose`
- Sends an `initialize` control request before user messages.
- Streams events (assistant messages, tool use, system messages, etc.) to an
  optional handler during each turn.
- Automatically approves tool permission requests (`can_use_tool` → `allow`).

## Usage

```go
client, err := claude.Start(ctx, claude.Options{
    BinaryPath: "claude",
    WorkDir: "/workspace",
    SystemPrompt: "You are a helpful assistant.",
    Model: "claude-3-5-sonnet-20241022",
    MaxTurns: 20,
})
if err != nil {
    // handle error
}
defer client.Close()

result, err := client.Turn(ctx, claude.TurnParams{Prompt: "Hello!"}, func(ev claude.Event) {
    // Handle streaming events.
})
if err != nil {
    // handle error
}
fmt.Println(result.Response)
```

## Result Diagnostics

`TurnResult` preserves the CLI's `Subtype`, optional `APIErrorStatus` and
`TerminalReason`. The status pointer is nil when absent or null; older CLIs keep
the existing zero-value behavior. Unknown subtype/reason strings are preserved
for forward compatibility, not interpreted as success or retry instructions.
These fields match the [official SDK result metadata](https://github.com/anthropics/claude-agent-sdk-python/blob/main/src/claude_agent_sdk/types.py).

Always check `IsError`: an API failure can have subtype `success`. `Turn` still
returns the result without synthesizing a Go error or retrying the operation.
Callers should allowlist metadata before logging it and avoid logging response
text or raw error bodies. A restored session or an HTTP status alone does not
establish whether a failed turn performed external side effects.

## Notes

- Unknown message types are skipped to preserve forward compatibility.
- `tool_result.content` is stored as `json.RawMessage` to handle both string
  and array payloads.
- `usage` uses snake_case JSON keys; `modelUsage` uses camelCase.
- The SDK removes `CLAUDECODE` from inherited environment variables before
  launching the subprocess.
