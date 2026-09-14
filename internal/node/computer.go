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
func (h *Host) toScreen(x, y int) (int, int, error) {
	h.mu.RLock()
	g := h.lastFrame
	h.mu.RUnlock()
	if g.imgW == 0 || g.imgH == 0 {
		return 0, 0, fmt.Errorf("take a screenshot before sending coordinates")
	}
	sx := g.screenX + x*g.screenW/g.imgW
	sy := g.screenY + y*g.screenH/g.imgH
	return sx, sy, nil
}

type actParams struct {
	Action          string  `json:"action"`
	Coordinate      []int   `json:"coordinate"`
	StartCoordinate []int   `json:"startCoordinate"`
	Text            string  `json:"text"`
	ScrollDirection string  `json:"scrollDirection"`
	ScrollAmount    int     `json:"scrollAmount"`
	Duration        float64 `json:"duration"`
	ScreenIndex     int     `json:"screenIndex"`
	MaxWidth        int     `json:"maxWidth"`
	// x/y are accepted as a convenience alongside the documented coordinate array.
	X *int `json:"x"`
	Y *int `json:"y"`
}

func (p actParams) point() (int, int, bool) {
	if len(p.Coordinate) >= 2 {
		return p.Coordinate[0], p.Coordinate[1], true
	}
	if p.X != nil && p.Y != nil {
		return *p.X, *p.Y, true
	}
	return 0, 0, false
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
			return nil // click where the pointer already is
		}
		sx, sy, err := h.toScreen(x, y)
		if err != nil {
			return err
		}
		return mouseMove(sx, sy)
	}

	var err error
	switch action {
	case "screenshot":
		return h.screenSnapshot(raw)
	case "wait":
		d := p.Duration
		if d <= 0 {
			d = 1
		}
		if d > 30 {
			d = 30
		}
		time.Sleep(time.Duration(d * float64(time.Second)))
		return h.screenSnapshot(raw)
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
			err = mouseClick(button, count)
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
		if len(p.StartCoordinate) >= 2 {
			var sx, sy int
			if sx, sy, err = h.toScreen(p.StartCoordinate[0], p.StartCoordinate[1]); err == nil {
				if err = mouseMove(sx, sy); err == nil {
					err = mouseButton("left", true)
				}
			}
		} else {
			err = mouseButton("left", true)
		}
		if err == nil {
			time.Sleep(60 * time.Millisecond)
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
			err = mouseScroll(strings.ToLower(p.ScrollDirection), amount)
		}
	case "type":
		err = typeText(p.Text)
	case "key":
		err = pressKeys(p.Text)
	case "hold_key":
		d := p.Duration
		if d <= 0 {
			d = 0.5
		}
		err = holdKeys(p.Text, time.Duration(d*float64(time.Second)))
	case "":
		return nil, "computer.act needs an action"
	default:
		return nil, fmt.Sprintf("computer.act action %q is not implemented by ClawHQ", action)
	}
	if err != nil {
		return nil, err.Error()
	}
	return map[string]any{"ok": true, "action": action}, ""
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
