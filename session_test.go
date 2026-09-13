package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStartSessionSelection(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec \"$CLAUDE_SDK_TEST_BINARY\" -test.run '^TestSessionHelperProcess$' -- \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	const sessionID = "421ebd1c-3298-4b10-af6d-e5662a40fca9"
	for _, tc := range []struct {
		name string
		opts Options
		flag string
	}{
		{"unchanged default", Options{}, ""},
		{"new session", Options{SessionID: sessionID}, "--session-id=" + sessionID},
		{"resume session", Options{Resume: sessionID}, "--resume=" + sessionID},
		{"opaque resume selector", Options{Resume: "/private/session with spaces.jsonl"}, "--resume=/private/session with spaces.jsonl"},
		{"selector is not another flag", Options{Resume: "--fork-session"}, "--resume=--fork-session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--output-format", "stream-json", "--input-format", "stream-json", "--verbose"}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			encoded, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			opts := tc.opts
			opts.BinaryPath = shim
			opts.Stderr = &stderr
			opts.Env = []string{"CLAUDE_SDK_SESSION_HELPER=1", "CLAUDE_SDK_TEST_BINARY=" + binary, "CLAUDE_SDK_TEST_ARGS=" + string(encoded)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := Start(ctx, opts)
			if err != nil {
				t.Fatalf("start: %v; stderr: %s", err, stderr.String())
			}
			defer client.Close()
			result, err := client.Turn(ctx, TurnParams{Prompt: "follow-up"}, nil)
			if err != nil || result.SessionID != sessionID || result.Response != "follow-up" {
				t.Fatalf("turn = %+v, %v", result, err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("close: %v; stderr: %s", err, stderr.String())
			}
		})
	}
}

func TestStartRejectsConflictingSessionSelectionBeforeSpawn(t *testing.T) {
	_, err := Start(context.Background(), Options{BinaryPath: "/does-not-exist", SessionID: "new", Resume: "old"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected session-selection validation before subprocess start, got %v", err)
	}
}

func TestSessionHelperProcess(t *testing.T) {
	if os.Getenv("CLAUDE_SDK_SESSION_HELPER") != "1" {
		return
	}
	var expected []string
	if err := json.Unmarshal([]byte(os.Getenv("CLAUDE_SDK_TEST_ARGS")), &expected); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var actual []string
	for i, arg := range os.Args {
		if arg == "--" {
			actual = os.Args[i+1:]
			break
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		fmt.Fprintf(os.Stderr, "args = %q; want %q\n", actual, expected)
		os.Exit(2)
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Message   struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		var response any
		switch request.Type {
		case "control_request":
			response = map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": request.RequestID}}
		case "user":
			response = map[string]any{"type": "result", "session_id": "421ebd1c-3298-4b10-af6d-e5662a40fca9", "result": request.Message.Content}
		default:
			os.Exit(2)
		}
		if err := encoder.Encode(response); err != nil {
			os.Exit(2)
		}
	}
	if scanner.Err() != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
