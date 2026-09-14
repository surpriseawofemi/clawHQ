package node

import (
	"encoding/json"
	"strings"
	"testing"
)

func newTestHost(t *testing.T) *Host {
	t.Helper()
	return New(t.TempDir(), nil, nil, nil, nil)
}

// Off by default, and a refusal must say so rather than time out or act.
func TestComputerActRefusedWhenControlOff(t *testing.T) {
	h := newTestHost(t)
	_, failure := h.computerAct(json.RawMessage(`{"action":"left_click","x":10,"y":10}`))
	if !strings.Contains(failure, "switched off") {
		t.Fatalf("expected an explicit refusal, got %q", failure)
	}
}

// Screenshot pixels map back onto the real screen through the remembered frame,
// including a virtual-desktop origin that is not (0,0).
func TestToScreenMapsThroughLastFrame(t *testing.T) {
	h := newTestHost(t)
	h.rememberFrame(frameGeometry{imgW: 800, imgH: 450, screenX: -1920, screenY: 0, screenW: 1600, screenH: 900})
	x, y, err := h.toScreen(400, 225, 0)
	if err != nil {
		t.Fatal(err)
	}
	if x != -1920+800 || y != 450 {
		t.Fatalf("got (%d,%d), want (%d,%d)", x, y, -1920+800, 450)
	}
	// A caller measuring in a 400px-wide copy of the same frame says so via refWidth.
	x, y, err = h.toScreen(200, 112.5, 400)
	if err != nil {
		t.Fatal(err)
	}
	if x != -1920+800 || y != 450 {
		t.Fatalf("refWidth: got (%d,%d), want (%d,%d)", x, y, -1920+800, 450)
	}
}

func TestSystemNotifyForwardsToApp(t *testing.T) {
	var got Notification
	h := New(t.TempDir(), nil, nil, func(n Notification) { got = n }, nil)
	_, failure := h.systemNotify(json.RawMessage(`{"title":"Check the browser","body":"Log in for me"}`))
	if failure != "" {
		t.Fatal(failure)
	}
	if got.Title != "Check the browser" || got.Body != "Log in for me" {
		t.Fatalf("notification not forwarded: %+v", got)
	}
}
