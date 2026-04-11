package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
)

const maxScannerBuffer = 1024 * 1024

type transport struct {
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	writeMu sync.Mutex
}

func newTransport(stdin io.WriteCloser, stdout io.ReadCloser) *transport {
	return &transport{stdin: stdin, stdout: stdout}
}

func (t *transport) StartRead(ctx context.Context) (<-chan json.RawMessage, <-chan error) {
	messages := make(chan json.RawMessage)
	errs := make(chan error, 1)
	go func() {
		defer close(messages)
		defer close(errs)
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				_ = t.stdout.Close()
			case <-done:
			}
		}()
		scanner := bufio.NewScanner(t.stdout)
		buffer := make([]byte, 0, 64*1024)
		scanner.Buffer(buffer, maxScannerBuffer)
		for {
			if ctx.Err() != nil {
				return
			}
			if !scanner.Scan() {
				break
			}
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if !strings.HasPrefix(line, "{") {
				continue
			}
			payload := json.RawMessage(append([]byte(nil), line...))
			select {
			case <-ctx.Done():
				return
			case messages <- payload:
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errs <- err
		}
	}()
	return messages, errs
}

func (t *transport) WriteJSON(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return t.write(data)
}

func (t *transport) CloseWriter() error {
	return t.stdin.Close()
}

func (t *transport) write(data []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err := t.stdin.Write(data)
	return err
}
