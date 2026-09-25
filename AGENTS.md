# Repository Guidance

## Living Documentation

- Keep non-obvious contracts and rationale beside the owning Go code; update
  comments and tests with behavior changes instead of narrating the implementation.
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
- Without Navigator, use native Go package structure and documentation, then
  targeted source/test reads. Do not require any particular workspace or lab path.

## Verification

- Use the Go version required by `go.mod`. For behavior changes, run focused
  credential-free tests with `go test -mod=readonly`; add `-race` for lifecycle work.
- Use a disposable `HOME` and an allowlisted environment without inherited CLI
  state, authentication, or live-fixture overrides. Native acceptance requires
  separate explicit authorization; never enable it for ordinary local verification.
- Run `gofmt` on edited Go files and `git diff --check`. For documentation-only
  work, compare source tokens and build/tool directives with the base. Avoid
  `go.mod`/`go.sum` changes and generated output; report test results and skips.
