//go:build windows

package node

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Mouse and keyboard injection through SendInput. Same rule as the screen capture:
// plain syscalls, no CGO, so ClawHQ stays one self-contained binary.
var (
	procSendInput    = user32.NewProc("SendInput")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procVkKeyScanW   = user32.NewProc("VkKeyScanW")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000
	wheelDelta      = 120

	keyEventKeyUp   = 0x0002
	keyEventUnicode = 0x0004
)

// INPUT on 64-bit Windows is 40 bytes: a DWORD type, 4 bytes of alignment padding,
// then a 32-byte union sized by MOUSEINPUT. The keyboard variant is smaller, so it is
// written into the same buffer and the tail left zero.
type mouseInput struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

type keybdInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

type winInput struct {
	Type uint32
	_    uint32
	Mi   mouseInput
}

func sendInputs(inputs []winInput) error {
	if len(inputs) == 0 {
		return nil
	}
	n, _, callErr := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(winInput{}),
	)
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput delivered %d of %d events: %v", n, len(inputs), callErr)
	}
	return nil
}

func mouseEvent(flags uint32, data uint32) winInput {
	return winInput{Type: inputMouse, Mi: mouseInput{Flags: flags, MouseData: data}}
}

func keyEvent(vk uint16, scan uint16, flags uint32) winInput {
	in := winInput{Type: inputKeyboard}
	kb := (*keybdInput)(unsafe.Pointer(&in.Mi))
	kb.Vk, kb.Scan, kb.Flags = vk, scan, flags
	return in
}

func inputSupported() bool { return true }

func mouseMove(x, y int) error {
	ok, _, err := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if ok == 0 {
		return fmt.Errorf("SetCursorPos(%d,%d): %v", x, y, err)
	}
	return nil
}

func buttonFlags(button string) (down, up uint32, err error) {
	switch button {
	case "left", "":
		return mouseLeftDown, mouseLeftUp, nil
	case "right":
		return mouseRightDown, mouseRightUp, nil
	case "middle":
		return mouseMiddleDown, mouseMiddleUp, nil
	}
	return 0, 0, fmt.Errorf("unknown mouse button %q", button)
}

func mouseButton(button string, down bool) error {
	d, u, err := buttonFlags(button)
	if err != nil {
		return err
	}
	if down {
		return sendInputs([]winInput{mouseEvent(d, 0)})
	}
	return sendInputs([]winInput{mouseEvent(u, 0)})
}

func mouseClick(button string, count int) error {
	d, u, err := buttonFlags(button)
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		if err := sendInputs([]winInput{mouseEvent(d, 0), mouseEvent(u, 0)}); err != nil {
			return err
		}
		if i+1 < count {
			// Well inside the default double-click time (500ms) but not instantaneous,
			// which some apps ignore.
			time.Sleep(60 * time.Millisecond)
		}
	}
	return nil
}

func mouseScroll(direction string, ticks int) error {
	var flag uint32 = mouseWheel
	delta := int32(wheelDelta * ticks)
	switch direction {
	case "down", "":
		delta = -delta
	case "up":
	case "left":
		flag = mouseHWheel
		delta = -delta
	case "right":
		flag = mouseHWheel
	default:
		return fmt.Errorf("unknown scroll direction %q", direction)
	}
	return sendInputs([]winInput{mouseEvent(flag, uint32(delta))})
}

// typeText injects text as Unicode key events, which works regardless of the active
// keyboard layout and covers characters with no key at all.
func typeText(text string) error {
	if text == "" {
		return nil
	}
	var inputs []winInput
	for _, unit := range utf16.Encode([]rune(text)) {
		if unit == '\n' {
			inputs = append(inputs, keyEvent(vkReturn, 0, 0), keyEvent(vkReturn, 0, keyEventKeyUp))
			continue
		}
		inputs = append(inputs,
			keyEvent(0, unit, keyEventUnicode),
			keyEvent(0, unit, keyEventUnicode|keyEventKeyUp),
		)
	}
	// Send in modest batches so a long paste does not overrun the input queue.
	for len(inputs) > 0 {
		n := len(inputs)
		if n > 64 {
			n = 64
		}
		if err := sendInputs(inputs[:n]); err != nil {
			return err
		}
		inputs = inputs[n:]
	}
	return nil
}

const (
	vkBack    = 0x08
	vkTab     = 0x09
	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12 // alt
	vkEscape  = 0x1B
	vkSpace   = 0x20
	vkPrior   = 0x21
	vkNext    = 0x22
	vkEnd     = 0x23
	vkHome    = 0x24
	vkLeft    = 0x25
	vkUp      = 0x26
	vkRight   = 0x27
	vkDown    = 0x28
	vkInsert  = 0x2D
	vkDelete  = 0x2E
	vkLWin    = 0x5B
	vkF1      = 0x70
)

var namedKeys = map[string]uint16{
	"ctrl": vkControl, "control": vkControl,
	"shift": vkShift,
	"alt":   vkMenu, "option": vkMenu,
	"win": vkLWin, "super": vkLWin, "meta": vkLWin, "cmd": vkLWin, "command": vkLWin,
	"return": vkReturn, "enter": vkReturn,
	"tab": vkTab, "escape": vkEscape, "esc": vkEscape,
	"backspace": vkBack, "delete": vkDelete, "del": vkDelete, "insert": vkInsert,
	"space": vkSpace,
	"up":    vkUp, "down": vkDown, "left": vkLeft, "right": vkRight,
	"home": vkHome, "end": vkEnd, "pageup": vkPrior, "page_up": vkPrior, "pagedown": vkNext, "page_down": vkNext,
}

// keyCode resolves one token of a combo like "ctrl+shift+t" to a virtual key. Single
// characters go through VkKeyScanW so the layout decides; a returned shift state is
// honoured by the caller adding vkShift.
func keyCode(token string) (vk uint16, needShift bool, err error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if vk, ok := namedKeys[t]; ok {
		return vk, false, nil
	}
	if len(t) >= 2 && t[0] == 'f' {
		var n int
		if _, scanErr := fmt.Sscanf(t, "f%d", &n); scanErr == nil && n >= 1 && n <= 24 {
			return uint16(vkF1 + n - 1), false, nil
		}
	}
	runes := []rune(token)
	if len(runes) == 1 {
		r, _, _ := procVkKeyScanW.Call(uintptr(uint16(runes[0])))
		res := uint16(r)
		if res == 0xFFFF {
			return 0, false, fmt.Errorf("no key for %q on this layout", token)
		}
		return res & 0xFF, res&0x100 != 0, nil
	}
	return 0, false, fmt.Errorf("unknown key %q", token)
}

func comboCodes(combo string) ([]uint16, error) {
	parts := strings.Split(combo, "+")
	if strings.HasSuffix(combo, "++") { // literal plus as the last key
		parts = append(strings.Split(strings.TrimSuffix(combo, "++"), "+"), "+")
	}
	var codes []uint16
	for i, part := range parts {
		if part == "" {
			continue
		}
		vk, shift, err := keyCode(part)
		if err != nil {
			return nil, err
		}
		if shift && i == len(parts)-1 {
			codes = append(codes, vkShift)
		}
		codes = append(codes, vk)
	}
	if len(codes) == 0 {
		return nil, fmt.Errorf("empty key combo")
	}
	return codes, nil
}

func pressKeys(combo string) error {
	codes, err := comboCodes(combo)
	if err != nil {
		return err
	}
	var inputs []winInput
	for _, vk := range codes {
		inputs = append(inputs, keyEvent(vk, 0, 0))
	}
	for i := len(codes) - 1; i >= 0; i-- {
		inputs = append(inputs, keyEvent(codes[i], 0, keyEventKeyUp))
	}
	return sendInputs(inputs)
}

func holdKeys(combo string, d time.Duration) error {
	codes, err := comboCodes(combo)
	if err != nil {
		return err
	}
	var down, up []winInput
	for _, vk := range codes {
		down = append(down, keyEvent(vk, 0, 0))
	}
	for i := len(codes) - 1; i >= 0; i-- {
		up = append(up, keyEvent(codes[i], 0, keyEventKeyUp))
	}
	if err := sendInputs(down); err != nil {
		return err
	}
	time.Sleep(d)
	return sendInputs(up)
}

// screenGeometry reports the virtual desktop origin and size, the space
// SetCursorPos works in.
func screenGeometry() (x, y, w, h int) {
	return int(systemMetric(smXVirtualScreen)), int(systemMetric(smYVirtualScreen)),
		int(systemMetric(smCXVirtualScreen)), int(systemMetric(smCYVirtualScreen))
}

// showNotification raises a toast through PowerShell. Best effort: a machine without
// the WinRT toast surface just gets nothing, and the in-app banner still shows.
func showNotification(title, body string) error {
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml('<toast><visual><binding template="ToastGeneric"><text>%s</text><text>%s</text></binding></visual></toast>')
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('ClawHQ').Show([Windows.UI.Notifications.ToastNotification]::new($xml))
`, xmlEscape(title), xmlEscape(body))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "'", "&apos;", `"`, "&quot;")
	return r.Replace(s)
}
