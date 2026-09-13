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

## Resume a Session

Preserve the CLI's session storage and pass the previous `TurnResult.SessionID`
as `Options.Resume` when starting a replacement subprocess. For a new session,
`Options.SessionID` can assign a UUID before the first turn. These options are
mutually exclusive; leaving both unset preserves the existing startup behavior.

```go
client, err := claude.Start(ctx, claude.Options{
    WorkDir: "/workspace",
    Resume: previousSessionID,
})
```

These options forward the CLI's [session selection flags](https://code.claude.com/docs/en/cli-reference).
They do not persist or copy session files, retry a failed resume, or establish
whether an interrupted turn already performed external side effects. Keep
unrelated sessions in separate state directories and coordinate one writer per
session. Missing or ambiguous state needs application-level reconciliation.

An opt-in native acceptance test replaces the CLI process between two text-only
turns and verifies recovery of the same session and a random marker. It disables
tools and isolates configuration, workspace and native session storage. Set
`CLAUDE_SDK_LIVE_SESSION=true`, `CLAUDE_SDK_LIVE_BINARY` to an absolute CLI path,
`CLAUDE_SDK_LIVE_STATE_ROOT` to an existing private directory, and
`CLAUDE_SDK_LIVE_OAUTH_TOKEN` through a private environment binding to an existing
subscription. Then run `go test -run '^TestLiveSessionResumption$' -count=1 -v`.
Do not put the token in a command line, shell history or a committed file. The
test retains its isolated state and `evidence.json`; the normal suite skips it.

## Notes

- Unknown message types are skipped to preserve forward compatibility.
- `tool_result.content` is stored as `json.RawMessage` to handle both string
  and array payloads.
- `usage` uses snake_case JSON keys; `modelUsage` uses camelCase.
- The SDK removes `CLAUDECODE` from inherited environment variables before
  launching the subprocess.
