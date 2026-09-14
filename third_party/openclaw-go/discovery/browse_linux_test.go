//go:build linux

package discovery

// browse_linux_test.go tests the Linux-specific defaultRunCmd implementation.
// It invokes real OS commands so we can verify the thin exec wrapper works.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDefaultRunCmd_Echo verifies that defaultRunCmd correctly executes a
// command and returns its stdout.  Using "echo" as a universally available
// command avoids a dependency on avahi-browse in CI.
func TestDefaultRunCmd_Echo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := defaultRunCmd(ctx, "echo", "hello", "openclaw")
	if err != nil {
		t.Fatalf("defaultRunCmd(echo): %v", err)
	}
	if !strings.Contains(out, "hello openclaw") {
		t.Errorf("output = %q, want to contain 'hello openclaw'", out)
	}
}

// TestDefaultRunCmd_ContextCancel verifies that defaultRunCmd respects context
// cancellation and terminates the child process promptly.
func TestDefaultRunCmd_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// "sleep 10" should be killed when the context times out.
	start := time.Now()
	_, err := defaultRunCmd(ctx, "sleep", "10")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from context cancellation, got nil")
	}
	// The sleep should not have run anywhere near 10 seconds.
	if elapsed > 2*time.Second {
		t.Errorf("defaultRunCmd took %v, want < 2s (context cancellation ignored?)", elapsed)
	}
}

// TestDefaultRunCmd_CommandNotFound verifies that a missing binary returns an
// error rather than silently succeeding.
func TestDefaultRunCmd_CommandNotFound(t *testing.T) {
	ctx := context.Background()
	_, err := defaultRunCmd(ctx, "this-binary-definitely-does-not-exist-abc123")
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}
