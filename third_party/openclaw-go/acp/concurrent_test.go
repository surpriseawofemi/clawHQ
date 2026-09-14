package acp

// concurrent_test.go tests that the ACP server's writeMu serialises concurrent
// writes from multiple goroutines without corrupting the output stream.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentSendNotification calls SendNotification from N goroutines at
// once while the server is also writing a response to a request.  Under -race
// this validates that writeMu prevents interleaved JSON lines in the output.
func TestConcurrentSendNotification(t *testing.T) {
	const N = 20

	handler := &mockHandler{
		initResp: &InitializeResponse{ProtocolVersion: ProtocolVersion},
	}

	// A pipe lets us control input precisely while the server is running.
	pr, pw := io.Pipe()
	output := &threadSafeBuffer{}

	srv := NewServer(handler, pr, output)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ctx)
	}()

	// Wait a tick for the server goroutine to start.
	time.Sleep(20 * time.Millisecond)

	// Fire N goroutines all calling SendNotification simultaneously.
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = srv.SendNotification(fmt.Sprintf("test/event%d", idx), map[string]int{"i": idx})
		}(i)
	}
	wg.Wait()

	// Also send a real request to exercise the interleaved-write path.
	req := RPCRequest{JSONRPC: "2.0", ID: "concurrent-req", Method: "initialize"}
	data, _ := json.Marshal(req)
	fmt.Fprintf(pw, "%s\n", data)

	// Give the server time to process.
	time.Sleep(100 * time.Millisecond)

	// Close input to cause Serve to return.
	pw.Close()

	select {
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return after pipe close")
	case err := <-serveErr:
		if err != nil && err != context.Canceled && !strings.Contains(err.Error(), "EOF") &&
			!strings.Contains(err.Error(), "io: read/write on closed pipe") {
			t.Errorf("Serve returned unexpected error: %v", err)
		}
	}

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: SendNotification error: %v", i, err)
		}
	}

	// Every line in the output must be valid JSON.
	raw := output.String()
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("output line is not valid JSON (concurrent writes interleaved?): %q", line)
		}
	}
}

// TestConcurrentSendRequestsFromHandler tests that a handler can call
// SendNotification concurrently with the server dispatching other requests
// without a race on writeMu.
func TestConcurrentNotificationsWhileServing(t *testing.T) {
	const N = 10

	var srv *Server // filled below

	handler := &mockHandler{
		initResp: &InitializeResponse{ProtocolVersion: ProtocolVersion},
	}

	pr, pw := io.Pipe()
	output := &threadSafeBuffer{}
	srv = NewServer(handler, pr, output)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ctx)
	}()

	time.Sleep(20 * time.Millisecond)

	// Blast notifications from a goroutine while also sending requests.
	var notifyWg sync.WaitGroup
	for i := 0; i < N; i++ {
		notifyWg.Add(1)
		go func(idx int) {
			defer notifyWg.Done()
			srv.SendNotification("background/ping", map[string]int{"seq": idx}) //nolint:errcheck
		}(i)
	}

	// Send a request from the main goroutine simultaneously.
	for i := 0; i < 3; i++ {
		req := RPCRequest{JSONRPC: "2.0", ID: fmt.Sprintf("req-%d", i), Method: "initialize"}
		data, _ := json.Marshal(req)
		fmt.Fprintf(pw, "%s\n", data)
	}

	notifyWg.Wait()
	time.Sleep(100 * time.Millisecond)
	pw.Close()

	select {
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return after pipe close")
	case <-serveErr:
	}

	// Verify output lines are all valid JSON.
	raw := output.String()
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("malformed JSON line (concurrent write interleaving?): %q", line)
		}
	}
}

// threadSafeBuffer is a bytes.Buffer protected by a mutex for concurrent use.
type threadSafeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *threadSafeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *threadSafeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
