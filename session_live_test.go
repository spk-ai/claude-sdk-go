package claude

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This opt-in test makes exactly two text-only turns through a real CLI. It
// never reads the caller's credential files or reuses their CLI state directory.
func TestLiveSessionResumption(t *testing.T) {
	if os.Getenv("CLAUDE_SDK_LIVE_SESSION") != "true" {
		t.Skip("requires explicit live-session acceptance opt-in")
	}
	token := os.Getenv("CLAUDE_SDK_LIVE_OAUTH_TOKEN")
	root := os.Getenv("CLAUDE_SDK_LIVE_STATE_ROOT")
	binary := os.Getenv("CLAUDE_SDK_LIVE_BINARY")
	if token == "" || !filepath.IsAbs(root) || !filepath.IsAbs(binary) {
		t.Fatal("explicit subscription token, absolute state root and CLI binary are required")
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_CUSTOM_HEADERS",
		"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		if os.Getenv(name) != "" {
			t.Fatalf("refusing conflicting authentication/provider variable %s", name)
		}
	}
	directory, err := os.MkdirTemp(root, "claude-sdk-session-")
	if err != nil {
		t.Fatal(err)
	}
	home, state, workspace := filepath.Join(directory, "home"), filepath.Join(directory, "state"), filepath.Join(directory, "workspace")
	for _, path := range []string{home, state, workspace} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("retained acceptance directory: %s", directory)
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", state)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", token)
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")
	t.Setenv("DISABLE_AUTOUPDATER", "1")
	shim := filepath.Join(directory, "claude-no-tools")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec \"$CLAUDE_SDK_LIVE_BINARY\" --tools \"\" \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil {
		t.Fatal("cannot determine native CLI version")
	}
	markerBytes := make([]byte, 12)
	if _, err := rand.Read(markerBytes); err != nil {
		t.Fatal(err)
	}
	marker := hex.EncodeToString(markerBytes)
	sessionBytes := make([]byte, 16)
	if _, err := rand.Read(sessionBytes); err != nil {
		t.Fatal(err)
	}
	sessionBytes[6] = sessionBytes[6]&0x0f | 0x40
	sessionBytes[8] = sessionBytes[8]&0x3f | 0x80
	h := hex.EncodeToString(sessionBytes)
	sessionID := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	opts := Options{BinaryPath: shim, WorkDir: workspace, SessionID: sessionID, MaxTurns: 1, Model: "sonnet"}
	first, err := Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	initial, err := first.Turn(ctx, TurnParams{Prompt: "Remember this exact marker for the next turn: " + marker + ". Reply only ACK."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if initial.IsError || initial.SessionID != sessionID || strings.TrimSpace(initial.Response) != "ACK" {
		t.Fatal("first native turn did not acknowledge the assigned session and marker")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	opts.SessionID, opts.Resume = "", sessionID
	second, err := Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	resumed, err := second.Turn(ctx, TurnParams{Prompt: "Return only the exact marker from my previous message."}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.IsError || resumed.SessionID != sessionID || strings.TrimSpace(resumed.Response) != marker {
		t.Fatal("replacement native process did not recover the same session and marker")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if first.cmd.Process.Pid == second.cmd.Process.Pid {
		t.Fatal("native CLI process was not replaced")
	}
	payload, err := json.MarshalIndent(map[string]any{"passed": true, "nativeVersion": strings.TrimSpace(string(version)),
		"sessionId": sessionID, "firstPid": first.cmd.Process.Pid, "replacementPid": second.cmd.Process.Pid,
		"markerRecovered": true, "turns": 2, "toolsEnabled": false, "directory": directory}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "evidence.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log(string(payload))
}
