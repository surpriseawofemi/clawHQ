package gateway

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A retry must not cancel the wait it belongs to. Before the fix, Connect ran
// Disconnect, which fired cancelPending — the loop's own context — so the first
// retry failed with "operation was canceled" and the phase dropped to error.
// Against an unreachable address the retry now fails for the honest reason and the
// phase stays pending, since a dial failure is transient.
func TestWaitForApprovalSurvivesItsOwnRetry(t *testing.T) {
	old := retryInterval
	retryInterval = 100 * time.Millisecond
	defer func() { retryInterval = old }()

	c, err := New(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.setStatus(func(s *Status) { s.Phase = PhasePending })

	c.WaitForApproval(context.Background(), "gw1", "ws://127.0.0.1:1", Credential{BootstrapToken: "x"})
	time.Sleep(600 * time.Millisecond)

	st := c.Status()
	if st.Phase != PhasePending {
		t.Fatalf("phase = %q (%s), want pending", st.Phase, st.Error)
	}
	if st.Error == "" || !strings.Contains(st.Error, "still waiting") {
		t.Fatalf("expected a transient-failure note, got %q", st.Error)
	}
	c.CancelApprovalWait()
}

func TestIsPairingRefused(t *testing.T) {
	for msg, want := range map[string]bool{
		"setup code invalid, expired, revoked, or already used": true,
		"device pairing rejected":                               true,
		"dial tcp: connection refused":                          false,
		"read challenge: EOF":                                   false,
	} {
		if got := isPairingRefused(errString(msg)); got != want {
			t.Errorf("%q: got %v want %v", msg, got, want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }
