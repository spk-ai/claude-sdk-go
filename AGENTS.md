# Repository Guidance

## Living Documentation

- Keep implemented contracts beside the owning Go package, exported API, or
  protocol handler. Document option defaults, process/session ownership, failure
  behavior, and CLI delegation limits; update comments and tests with changes.
- Keep Markdown for usage, setup, security, coordination, and verification;
  link to Go owners instead of repeating their implementation contracts.
- Maintain `docs/catalog.json` as a curated Markdown index, not a generated
  component inventory. Preserve historical records and the MIT `LICENSE`.

## Navigation

- When Navigator is available, first run `repos --worktrees`. Select this
  checkout's Git-discovered basename with `--worktree <basename>`, then run
  `scan --repo claude-sdk` and batch `inspect` related returned IDs with that
  selector before reading source. Use the same selector for `docs --repo claude-sdk`.
- Check returned branch/head/dirty provenance. Follow symbols, comments, and
  test links with focused excerpts; Go methods use `Type.Method`. Cross-repo
  `@see` targets use repo selectors and extensionless components; local targets
  retain their source extension and are repo-relative.
- For standalone upstream use without Navigator, fall back to native Go package
  structure, `git diff upstream/main --stat`, `rg --files`, `go list ./...`,
  `go doc`, and targeted source/test reads. Do not depend on Navigator or any
  absolute workspace/lab path.

## Verification

- Use the Go version required by `go.mod`. Run credential-free tests with
  `go test -mod=readonly ./...`; use `-race` for subprocess/session lifecycle work.
  `session_test.go` uses a local helper process rather than a provider CLI.
- Use a disposable `HOME`, clear inherited CLI state/auth overrides, and leave
  `CLAUDE_SDK_LIVE_SESSION` unset. The native session acceptance fixture requires
  separate explicit authorization; never access provider credentials or enable
  external fixtures for ordinary local verification.
- Run `gofmt` on edited Go files and `git diff --check`. For documentation-only
  work, verify source tokens/AST are unchanged apart from comments. Avoid
  `go.mod`/`go.sum` changes and generated output; report test results and skips.
