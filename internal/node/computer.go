package node

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Desktop control commands. Both names are OpenClaw's own — computer.act and
// system.notify sit in the gateway's default allowlist for windows and macOS, so a
// node advertising them is reachable without any gateway-side config.
const (
	CmdComputerAct  = "computer.act"
	CmdSystemNotify = "system.notify"
)

// Notification is a system.notify request an agent sent to this machine. It is
// forwarded to the frontend so the app can show it, as well as to the OS.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	AtMs  int64  `json:"atMs"`
}

// frameGeometry remembers how the last screenshot maps onto the real screen, so
// coordinates given in screenshot pixels (which is what computer.act specifies) land
// on the right spot even when the image was downscaled.
type frameGeometry struct {
	imgW, imgH       int
	screenX, screenY int
	screenW, screenH int
	frameID          string
}

// SetDesktopControl turns computer.act on or off. The command stays advertised
// either way — changing the advertised list forces a re-pair — but invokes are
// refused while it is off, which is the safe default.
func (h *Host) SetDesktopControl(on bool) {
	h.mu.Lock()
	h.desktopControl = on
	h.mu.Unlock()
	h.setStatus(func(s *Status) { s.DesktopControl = on })
}

func (h *Host) rememberFrame(g frameGeometry) {
	h.mu.Lock()
	h.lastFrame = g
	h.mu.Unlock()
}

// toScreen maps a screenshot-pixel coordinate onto the screen.
//
// refWidth, when given, is the width of the image the caller measured x/y in; the
// contract lets a caller work from a downscaled frame and say so. Without it the
// most recent screenshot this node produced is the reference.
func (h *Host) toScreen(x, y float64, refWidth int) (int, int, error) {
	h.mu.RLock()
	g := h.lastFrame
	h.mu.RUnlock()
	if g.screenW == 0 || g.screenH == 0 {
		sx, sy, sw, sh := screenGeometry()
		g = frameGeometry{screenX: sx, screenY: sy, screenW: sw, screenH: sh, imgW: sw, imgH: sh}
	}
	if g.screenW == 0 || g.screenH == 0 {
		return 0, 0, fmt.Errorf("screen size unknown — take a screenshot first")
	}
	var scale float64
	switch {
	case refWidth > 0:
		scale = float64(g.screenW) / float64(refWidth)
	case g.imgW > 0:
		scale = float64(g.screenW) / float64(g.imgW)
	default:
		return 0, 0, fmt.Errorf("take a screenshot before sending coordinates")
	}
	sx := g.screenX + int(x*scale+0.5)
	sy := g.screenY + int(y*scale+0.5)
	return sx, sy, nil
}

// actParams mirrors the gateway's computer.act v1 input schema: x/y in screenshot
// pixels (with optional refWidth), `keys` for key combos, fromX/fromY for drags,
// durationMs for holds. The gateway rejects unknown fields before forwarding, so a
// caller cannot reach this code with anything else.
type actParams struct {
	Action          string   `json:"action"`
	X               *float64 `json:"x"`
	Y               *float64 `json:"y"`
	FromX           *float64 `json:"fromX"`
	FromY           *float64 `json:"fromY"`
	RefWidth        int      `json:"refWidth"`
	ScreenIndex     int      `json:"screenIndex"`
	DisplayFrameID  string   `json:"displayFrameId"`
	Modifiers       string   `json:"modifiers"`
	Text            string   `json:"text"`
	Keys            string   `json:"keys"`
	ScrollDirection string   `json:"scrollDirection"`
	ScrollAmount    int      `json:"scrollAmount"`
	DurationMs      int      `json:"durationMs"`
}

func (p actParams) point() (float64, float64, bool) {
	if p.X != nil && p.Y != nil {
		return *p.X, *p.Y, true
	}
	return 0, 0, false
}

// combo returns the key combo for key/hold_key. `keys` is the contract field; `text`
// is accepted for callers written against the older description.
func (p actParams) combo() string {
	if strings.TrimSpace(p.Keys) != "" {
		return p.Keys
	}
	return p.Text
}

// computerAct answers a computer.act invoke with the pointer, keyboard, scroll and
// observation actions from the v2 contract. Window and browser families are not
// implemented; they come back as a clear refusal rather than a timeout.
func (h *Host) computerAct(raw json.RawMessage) (any, string) {
	h.mu.RLock()
	allowed := h.desktopControl
	h.mu.RUnlock()
	if !allowed {
		return nil, "desktop control is switched off on this machine — enable it in ClawHQ → Settings → node"
	}
	if !inputSupported() {
		return nil, "desktop control is not implemented on this platform yet"
	}

	var p actParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "bad computer.act params: " + err.Error()
	}
	action := strings.ToLower(strings.TrimSpace(p.Action))

	moveTo := func() error {
		x, y, ok := p.point()
		if !ok {
			return nil // act where the pointer already is
		}
		sx, sy, err := h.toScreen(x, y, p.RefWidth)
		if err != nil {
			return err
		}
		return mouseMove(sx, sy)
	}
	// Modifier keys held for the duration of a pointer action, e.g. "ctrl+shift".
	withModifiers := func(fn func() error) error {
		mods := strings.TrimSpace(p.Modifiers)
		if mods == "" {
			return fn()
		}
		if err := holdKeysBegin(mods); err != nil {
			return err
		}
		defer holdKeysEnd(mods)
		return fn()
	}

	var err error
	switch action {
	case "mouse_move":
		err = moveTo()
	case "left_click", "right_click", "middle_click", "double_click", "triple_click":
		if err = moveTo(); err == nil {
			button := strings.TrimSuffix(action, "_click")
			count := 1
			switch action {
			case "double_click":
				button, count = "left", 2
			case "triple_click":
				button, count = "left", 3
			}
			err = withModifiers(func() error { return mouseClick(button, count) })
		}
	case "left_mouse_down":
		if err = moveTo(); err == nil {
			err = mouseButton("left", true)
		}
	case "left_mouse_up":
		if err = moveTo(); err == nil {
			err = mouseButton("left", false)
		}
	case "left_click_drag":
		if p.FromX != nil && p.FromY != nil {
			var sx, sy int
			if sx, sy, err = h.toScreen(*p.FromX, *p.FromY, p.RefWidth); err == nil {
				if err = mouseMove(sx, sy); err == nil {
					err = mouseButton("left", true)
				}
			}
		} else {
			err = mouseButton("left", true)
		}
		if err == nil {
			pause := time.Duration(p.DurationMs) * time.Millisecond
			if pause <= 0 {
				pause = 60 * time.Millisecond
			}
			time.Sleep(pause)
			if err = moveTo(); err == nil {
				time.Sleep(60 * time.Millisecond)
				err = mouseButton("left", false)
			}
		}
	case "scroll":
		if err = moveTo(); err == nil {
			amount := p.ScrollAmount
			if amount <= 0 {
				amount = 3
			}
			err = withModifiers(func() error { return mouseScroll(strings.ToLower(p.ScrollDirection), amount) })
		}
	case "type":
		err = typeText(p.Text)
	case "key":
		err = pressKeys(p.combo())
	case "hold_key":
		d := time.Duration(p.DurationMs) * time.Millisecond
		if d <= 0 {
			d = 500 * time.Millisecond
		}
		err = holdKeys(p.combo(), d)
	case "":
		return nil, "computer.act needs an action"
	default:
		return nil, fmt.Sprintf("computer.act action %q is not implemented by ClawHQ", action)
	}
	if err != nil {
		return nil, err.Error()
	}
	// Shape follows the contract's result schema: ok, effect, details. Input goes
	// through SendInput with no feedback channel, so the honest effect is unverifiable.
	return map[string]any{
		"ok":      true,
		"effect":  "unverifiable",
		"details": map[string]any{"action": action},
	}, ""
}

// systemNotify shows a notification on this machine and hands it to the app.
//
// Params: {title, body} — a plain {message} is accepted too.
func (h *Host) systemNotify(raw json.RawMessage) (any, string) {
	var p struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &p)
	if p.Body == "" {
		p.Body = p.Message
	}
	if p.Title == "" {
		p.Title = "An agent needs you"
	}
	if strings.TrimSpace(p.Body) == "" && strings.TrimSpace(p.Title) == "" {
		return nil, "system.notify needs a title or body"
	}

	n := Notification{Title: p.Title, Body: p.Body, AtMs: time.Now().UnixMilli()}
	if h.onNotify != nil {
		h.onNotify(n)
	}
	// The OS notification is best effort: the in-app banner is the reliable path.
	_ = showNotification(p.Title, p.Body)
	return map[string]any{"ok": true}, ""
}
