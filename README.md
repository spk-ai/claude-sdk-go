# Claude Code Go SDK

## Overview

Treat agent tools as trusted code: supply an execution sandbox and credentials
whose authority you intend to grant to those tools. The SDK is not a
tool-permission security boundary; see the [client contract](client.go).

## Usage

Use the Go version required by [go.mod](go.mod) and an installed
[Claude Code CLI](https://code.claude.com/docs/en/cli-reference) with a separately
authorized account. API usage belongs in [client.go](client.go) and
[options.go](options.go); [client_test.go](client_test.go) supplies executable
local protocol examples.

## Resume a Session

Keep native CLI state on private durable storage when replacing a process.
Separate unrelated sessions and coordinate one writer per session. Missing or
ambiguous state and possible side effects of interrupted turns require
application-level reconciliation; selectors are documented in
[options.go](options.go).

Native acceptance requires separate authorization, an installed CLI, a private
state root, and a subscription credential. Configure the opt-in inputs documented
by [session_live_test.go](session_live_test.go) through private environment
bindings, then run `go test -run '^TestLiveSessionResumption$' -count=1 -v`.
Keep credentials out of command lines, shell history, and committed files;
protect retained acceptance artifacts. Text-only continuity does not establish
safe replay of tool side effects or fencing of an old writer.

## Result Diagnostics

See [TurnResult and Turn](client.go), [ResultMessage](types.go), and the
[diagnostic contract tests](result_diagnostics_test.go) for result semantics.
Callers should allowlist metadata before logging it and avoid logging response
text or raw error bodies. A restored session or an HTTP status alone does not
establish whether a failed turn performed external side effects.

This integration checkout combines the independently
reviewable session-selection and diagnostic patches for local integration
acceptance. It is not a proposed bundled upstream change.

## Notes

Contribution and credential-free verification policy: [AGENTS.md](AGENTS.md).
