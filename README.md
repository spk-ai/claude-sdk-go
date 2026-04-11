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

## Notes

- Unknown message types are skipped to preserve forward compatibility.
- `tool_result.content` is stored as `json.RawMessage` to handle both string
  and array payloads.
- `usage` uses snake_case JSON keys; `modelUsage` uses camelCase.
- The SDK removes `CLAUDECODE` from inherited environment variables before
  launching the subprocess.
