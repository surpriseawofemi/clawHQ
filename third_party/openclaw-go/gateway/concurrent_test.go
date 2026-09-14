package gateway

// concurrent_test.go validates that concurrent uses of the gateway client are
// safe under the Go race detector (-race).  These tests exercise the locking
// around the pending-request map (pendingMu) and the WebSocket write path
// (connMu), as well as the double-close guard on the `done` channel when both
// client.Close() and the readLoop try to shut things down at the same time.

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/a3tai/openclaw-go/protocol"
	"github.com/gorilla/websocket"
)

// TestConcurrentSends fires N goroutines all calling client.Send()
// simultaneously on the same connected client.  Under -race this verifies that
// pendingMu and connMu are correctly held during registration, writing, and
// response dispatch.
func TestConcurrentSends(t *testing.T) {
	const N = 20

	mg, wsURL, cleanup := startMockGateway(t)
	defer cleanup()

	// Gateway echoes every request back as an OK response immediately.
	mg.onRequest = func(conn *websocket.Conn, req protocol.Request) {
		respData, _ := protocol.MarshalResponse(req.ID, map[string]string{"ok": "true"})
		conn.WriteMessage(websocket.TextMessage, respData)
	}

	client := NewClient(WithToken("tok"), WithConnectTimeout(5*time.Second))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := client.Send(ctx, fmt.Sprintf("test.method.%d", idx), map[string]int{"n": idx})
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Send error: %v", i, err)
		}
	}
}

// TestConcurrentSendEvent fires N goroutines all calling client.SendEvent()
// simultaneously.  This specifically stresses connMu since SendEvent holds
// it for the entire WriteMessage call.
func TestConcurrentSendEvent(t *testing.T) {
	const N = 20

	_, wsURL, cleanup := startMockGateway(t)
	defer cleanup()

	client := NewClient(WithToken("tok"), WithConnectTimeout(5*time.Second))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = client.SendEvent(fmt.Sprintf("test.event.%d", idx), map[string]int{"n": idx})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: SendEvent error: %v", i, err)
		}
	}
}

// TestCloseRacesWithConnectionError triggers a race between client.Close() and
// the readLoop attempting to close the done channel after the server-side
// connection drops.  If the double-close guard in Close() is not concurrency-
// safe, this test will panic with "close of closed channel".
func TestCloseRacesWithConnectionError(t *testing.T) {
	// Run several iterations to increase the chance of hitting the race window.
	for i := 0; i < 20; i++ {
		func() {
			mg, wsURL, cleanup := startMockGateway(t)
			defer cleanup()

			client := NewClient(WithToken("tok"), WithConnectTimeout(5*time.Second))

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := client.Connect(ctx, wsURL); err != nil {
				t.Fatalf("iteration %d: Connect: %v", i, err)
			}

			mg.waitReady(t)
			mg.mu.Lock()
			serverConn := mg.conns[len(mg.conns)-1]
			mg.mu.Unlock()

			// Fire the connection-drop and the explicit Close() simultaneously.
			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				serverConn.Close() // readLoop gets an error → tries to close done
			}()

			go func() {
				defer wg.Done()
				client.Close() // user-facing close → also tries to close done
			}()

			wg.Wait()

			// The client must be shut down; Done() must be closed.
			select {
			case <-client.Done():
			case <-time.After(2 * time.Second):
				t.Errorf("iteration %d: client.Done() not closed after concurrent close", i)
			}
		}()
	}
}

// TestConcurrentSendsWithCancel starts N concurrent Sends with a short-lived
// context, then lets the context expire.  Verifies that every goroutine gets
// a cancellation error and that the deferred pending-map cleanup completes
// without a race on pendingMu.
func TestConcurrentSendsWithCancel(t *testing.T) {
	const N = 10

	mg, wsURL, cleanup := startMockGateway(t)
	defer cleanup()

	// Gateway deliberately never responds.
	mg.onRequest = func(conn *websocket.Conn, req protocol.Request) {}

	client := NewClient(WithToken("tok"), WithConnectTimeout(5*time.Second))
	defer client.Close()

	outerCtx, outerCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer outerCancel()

	if err := client.Connect(outerCtx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	sendCtx, sendCancel := context.WithTimeout(outerCtx, 150*time.Millisecond)
	defer sendCancel()

	var wg sync.WaitGroup
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := client.Send(sendCtx, "hanging.method", nil)
			errs[idx] = err
		}(i)
	}

	// Let all goroutines block in Send, then cancel.
	time.Sleep(50 * time.Millisecond)
	sendCancel()

	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Errorf("goroutine %d: expected cancellation error, got nil", i)
		}
	}
}

// TestConcurrentSendAndClose verifies that calling Close() while a Send() is
// in-flight returns an error from Send without a race or panic on the done
// channel.
func TestConcurrentSendAndClose(t *testing.T) {
	mg, wsURL, cleanup := startMockGateway(t)
	defer cleanup()

	// Never respond.
	mg.onRequest = func(conn *websocket.Conn, req protocol.Request) {}

	client := NewClient(WithToken("tok"), WithConnectTimeout(5*time.Second))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var (
		sendErr error
		wg      sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, sendErr = client.Send(ctx, "slow.method", nil)
	}()

	// Let Send block in its select.
	time.Sleep(20 * time.Millisecond)
	client.Close()

	wg.Wait()

	if sendErr == nil {
		t.Fatal("expected error from Send after Close, got nil")
	}
}

// TestPendingMapCleanupOnCancel ensures that cancelled Sends always remove
// themselves from the pending map, so there is no accumulation of dead entries
// that could cause stale response matches later.
func TestPendingMapCleanupOnCancel(t *testing.T) {
	mg, wsURL, cleanup := startMockGateway(t)
	defer cleanup()

	// Never respond.
	mg.onRequest = func(conn *websocket.Conn, req protocol.Request) {}

	client := NewClient(WithToken("tok"), WithConnectTimeout(5*time.Second))
	defer client.Close()

	outerCtx, outerCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer outerCancel()

	if err := client.Connect(outerCtx, wsURL); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	const N = 5
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shortCtx, shortCancel := context.WithTimeout(outerCtx, 50*time.Millisecond)
			defer shortCancel()
			client.Send(shortCtx, "pending.test", nil) //nolint:errcheck
		}()
	}
	wg.Wait()

	// Give the deferred pending-map deletions time to complete.
	time.Sleep(100 * time.Millisecond)

	client.pendingMu.Lock()
	pendingLen := len(client.pending)
	client.pendingMu.Unlock()

	if pendingLen != 0 {
		t.Errorf("pending map has %d entries after all cancellations, want 0", pendingLen)
	}
}
