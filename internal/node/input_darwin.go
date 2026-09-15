//go:build darwin && cgo

package node

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreGraphics/CoreGraphics.h>
#include <stdint.h>

// One HID-state source for every event we post. Created lazily; a NULL source is
// still accepted by the Create* calls, so a failure here degrades rather than breaks.
static CGEventSourceRef clawSource(void) {
	static CGEventSourceRef src = NULL;
	if (src == NULL) {
		src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	}
	return src;
}

static int clawAXTrusted(int prompt) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *values[] = { prompt ? kCFBooleanTrue : kCFBooleanFalse };
	CFDictionaryRef opts = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	Boolean ok = AXIsProcessTrustedWithOptions(opts);
	CFRelease(opts);
	return ok ? 1 : 0;
}

static void clawCursor(double *x, double *y) {
	CGEventRef e = CGEventCreate(clawSource());
	CGPoint p = CGEventGetLocation(e);
	CFRelease(e);
	*x = p.x;
	*y = p.y;
}

static void clawPostMouse(uint32_t type, uint32_t button, double x, double y, int clickState, uint64_t flags) {
	CGEventRef e = CGEventCreateMouseEvent(clawSource(), (CGEventType)type, CGPointMake(x, y), (CGMouseButton)button);
	if (clickState > 0) {
		CGEventSetIntegerValueField(e, kCGMouseEventClickState, clickState);
	}
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void clawPostScroll(int vertical, int horizontal, uint64_t flags) {
	CGEventRef e = CGEventCreateScrollWheelEvent(clawSource(), kCGScrollEventUnitLine, 2, vertical, horizontal);
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void clawPostKey(uint16_t code, int down, uint64_t flags) {
	CGEventRef e = CGEventCreateKeyboardEvent(clawSource(), (CGKeyCode)code, down ? true : false);
	CGEventSetFlags(e, (CGEventFlags)flags);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void clawPostUnicode(const uint16_t *units, int n, int down) {
	CGEventRef e = CGEventCreateKeyboardEvent(clawSource(), 0, down ? true : false);
	CGEventKeyboardSetUnicodeString(e, (UniCharCount)n, (const UniChar *)units);
	CGEventSetFlags(e, 0);
	CGEventPost(kCGHIDEventTap, e);
	CFRelease(e);
}

static void clawMainDisplayBounds(double *x, double *y, double *w, double *h) {
	CGRect r = CGDisplayBounds(CGMainDisplayID());
	*x = r.origin.x;
	*y = r.origin.y;
	*w = r.size.width;
	*h = r.size.height;
}
*/
import "C"

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
)

// Mouse and keyboard injection through CGEvent posted at the HID tap. Needs the
// Accessibility grant; without it CGEventPost silently drops everything.

// CGEventType values (CGEventTypes.h).
const (
	cgLeftMouseDown     = 1
	cgLeftMouseUp       = 2
	cgRightMouseDown    = 3
	cgRightMouseUp      = 4
	cgMouseMoved        = 5
	cgLeftMouseDragged  = 6
	cgRightMouseDragged = 7
	cgOtherMouseDown    = 25
	cgOtherMouseUp      = 26
	cgOtherMouseDragged = 27
	cgMouseButtonLeft   = 0
	cgMouseButtonRight  = 1
	cgMouseButtonCenter = 2
	cgFlagMaskShift     = 1 << 17
	cgFlagMaskControl   = 1 << 18
	cgFlagMaskAlternate = 1 << 19
	cgFlagMaskCommand   = 1 << 20
)

// Virtual key codes for the US ANSI layout (Carbon's kVK_ constants, restated so we
// do not drag Carbon headers into the build).
const (
	vkA            = 0
	vkS            = 1
	vkD            = 2
	vkF            = 3
	vkH            = 4
	vkG            = 5
	vkZ            = 6
	vkX            = 7
	vkC            = 8
	vkV            = 9
	vkB            = 11
	vkQ            = 12
	vkW            = 13
	vkE            = 14
	vkR            = 15
	vkY            = 16
	vkT            = 17
	vk1            = 18
	vk2            = 19
	vk3            = 20
	vk4            = 21
	vk6            = 22
	vk5            = 23
	vkEqual        = 24
	vk9            = 25
	vk7            = 26
	vkMinus        = 27
	vk8            = 28
	vk0            = 29
	vkRightBracket = 30
	vkO            = 31
	vkU            = 32
	vkLeftBracket  = 33
	vkI            = 34
	vkP            = 35
	vkReturn       = 36
	vkL            = 37
	vkJ            = 38
	vkQuote        = 39
	vkK            = 40
	vkSemicolon    = 41
	vkBackslash    = 42
	vkComma        = 43
	vkSlash        = 44
	vkN            = 45
	vkM            = 46
	vkPeriod       = 47
	vkTab          = 48
	vkSpace        = 49
	vkGrave        = 50
	vkBackspace    = 51
	vkEscape       = 53
	vkCommand      = 55
	vkShift        = 56
	vkOption       = 58
	vkControl      = 59
	vkF17          = 64
	vkF18          = 79
	vkF19          = 80
	vkF20          = 90
	vkF5           = 96
	vkF6           = 97
	vkF7           = 98
	vkF3           = 99
	vkF8           = 100
	vkF9           = 101
	vkF11          = 103
	vkF13          = 105
	vkF16          = 106
	vkF14          = 107
	vkF10          = 109
	vkF12          = 111
	vkF15          = 113
	vkHelp         = 114
	vkHome         = 115
	vkPageUp       = 116
	vkForwardDel   = 117
	vkF4           = 118
	vkEnd          = 119
	vkF2           = 120
	vkPageDown     = 121
	vkF1           = 122
	vkLeft         = 123
	vkRight        = 124
	vkDown         = 125
	vkUp           = 126
)

var (
	inputMu sync.Mutex
	// heldFlags are the modifiers kept down by holdKeysBegin. Events posted while
	// they are held carry them explicitly, since the system does not merge posted
	// modifier state into events we create afterwards.
	heldFlags uint64
	// heldButtons tracks which mouse buttons are down so a move becomes a drag.
	heldButtons [3]bool

	axMu       sync.Mutex
	axTrusted  bool
	axPrompted bool
)

// inputSupported reports whether the process has the Accessibility grant. The first
// call asks macOS to show the grant prompt; later calls only re-check while it is
// still refused, so a grant made mid-session is picked up.
func inputSupported() bool {
	axMu.Lock()
	defer axMu.Unlock()
	if axTrusted {
		return true
	}
	prompt := 0
	if !axPrompted {
		prompt = 1
		axPrompted = true
	}
	axTrusted = C.clawAXTrusted(C.int(prompt)) == 1
	return axTrusted
}

func cursorPos() (float64, float64) {
	var x, y C.double
	C.clawCursor(&x, &y)
	return float64(x), float64(y)
}

func postMouse(typ, button int, x, y float64, clickState int) {
	C.clawPostMouse(C.uint32_t(typ), C.uint32_t(button), C.double(x), C.double(y), C.int(clickState), C.uint64_t(heldFlags))
}

func mouseMove(x, y int) error {
	inputMu.Lock()
	defer inputMu.Unlock()
	typ, button := cgMouseMoved, cgMouseButtonLeft
	switch {
	case heldButtons[cgMouseButtonLeft]:
		typ = cgLeftMouseDragged
	case heldButtons[cgMouseButtonRight]:
		typ, button = cgRightMouseDragged, cgMouseButtonRight
	case heldButtons[cgMouseButtonCenter]:
		typ, button = cgOtherMouseDragged, cgMouseButtonCenter
	}
	postMouse(typ, button, float64(x), float64(y), 0)
	return nil
}

func buttonEvents(button string) (btn, down, up int, err error) {
	switch button {
	case "left", "":
		return cgMouseButtonLeft, cgLeftMouseDown, cgLeftMouseUp, nil
	case "right":
		return cgMouseButtonRight, cgRightMouseDown, cgRightMouseUp, nil
	case "middle":
		return cgMouseButtonCenter, cgOtherMouseDown, cgOtherMouseUp, nil
	}
	return 0, 0, 0, fmt.Errorf("unknown mouse button %q", button)
}

func mouseButton(button string, down bool) error {
	btn, d, u, err := buttonEvents(button)
	if err != nil {
		return err
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	x, y := cursorPos()
	if down {
		postMouse(d, btn, x, y, 1)
	} else {
		postMouse(u, btn, x, y, 1)
	}
	heldButtons[btn] = down
	return nil
}

func mouseClick(button string, count int) error {
	btn, d, u, err := buttonEvents(button)
	if err != nil {
		return err
	}
	if count < 1 {
		count = 1
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	x, y := cursorPos()
	for i := 0; i < count; i++ {
		// clickState is how apps tell a double click from two singles.
		postMouse(d, btn, x, y, i+1)
		postMouse(u, btn, x, y, i+1)
		if i+1 < count {
			time.Sleep(60 * time.Millisecond)
		}
	}
	return nil
}

// mouseScroll uses line units. CG's sign convention is positive = up / left, so
// "down" is negative: content moves as if the user rolled the wheel towards them.
func mouseScroll(direction string, ticks int) error {
	var v, h int
	switch direction {
	case "down", "":
		v = -ticks
	case "up":
		v = ticks
	case "left":
		h = ticks
	case "right":
		h = -ticks
	default:
		return fmt.Errorf("unknown scroll direction %q", direction)
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	C.clawPostScroll(C.int(v), C.int(h), C.uint64_t(heldFlags))
	return nil
}

// typeText sends the text as Unicode key events, so it works on any layout and for
// characters with no key. Newlines go as a real Return so forms and terminals react.
func typeText(text string) error {
	if text == "" {
		return nil
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	units := utf16.Encode([]rune(text))
	flush := func(chunk []uint16) {
		if len(chunk) == 0 {
			return
		}
		C.clawPostUnicode((*C.uint16_t)(&chunk[0]), C.int(len(chunk)), 1)
		C.clawPostUnicode((*C.uint16_t)(&chunk[0]), C.int(len(chunk)), 0)
	}
	var chunk []uint16
	for _, u := range units {
		if u == '\n' {
			flush(chunk)
			chunk = chunk[:0]
			C.clawPostKey(vkReturn, 1, C.uint64_t(heldFlags))
			C.clawPostKey(vkReturn, 0, C.uint64_t(heldFlags))
			continue
		}
		// The unicode string field holds 20 UTF-16 units; do not split a surrogate pair.
		if len(chunk) >= 20 || (len(chunk) == 19 && utf16.IsSurrogate(rune(u))) {
			flush(chunk)
			chunk = chunk[:0]
		}
		chunk = append(chunk, u)
	}
	flush(chunk)
	return nil
}

// comboKey is one token of a combo. Modifiers carry a flag mask; ordinary keys
// carry only a code, plus shift when the character needs it on a US layout.
type comboKey struct {
	code  uint16
	flag  uint64
	shift bool
}

var modifierKeys = map[string]comboKey{
	"cmd": {vkCommand, cgFlagMaskCommand, false}, "command": {vkCommand, cgFlagMaskCommand, false},
	"meta": {vkCommand, cgFlagMaskCommand, false}, "super": {vkCommand, cgFlagMaskCommand, false},
	"win":  {vkCommand, cgFlagMaskCommand, false},
	"ctrl": {vkControl, cgFlagMaskControl, false}, "control": {vkControl, cgFlagMaskControl, false},
	"alt": {vkOption, cgFlagMaskAlternate, false}, "option": {vkOption, cgFlagMaskAlternate, false},
	"shift": {vkShift, cgFlagMaskShift, false},
}

var namedKeys = map[string]uint16{
	"return": vkReturn, "enter": vkReturn,
	"tab": vkTab, "escape": vkEscape, "esc": vkEscape,
	"backspace": vkBackspace, "delete": vkForwardDel, "del": vkForwardDel,
	"insert": vkHelp, // no Insert on a Mac keyboard; Help sits in its slot
	"space":  vkSpace,
	"up":     vkUp, "down": vkDown, "left": vkLeft, "right": vkRight,
	"home": vkHome, "end": vkEnd,
	"pageup": vkPageUp, "page_up": vkPageUp, "pagedown": vkPageDown, "page_down": vkPageDown,
}

var fnKeys = [...]uint16{vkF1, vkF2, vkF3, vkF4, vkF5, vkF6, vkF7, vkF8, vkF9, vkF10,
	vkF11, vkF12, vkF13, vkF14, vkF15, vkF16, vkF17, vkF18, vkF19, vkF20}

// charKeys is the US ANSI layout: unshifted characters to key codes.
var charKeys = map[rune]uint16{
	'a': vkA, 'b': vkB, 'c': vkC, 'd': vkD, 'e': vkE, 'f': vkF, 'g': vkG, 'h': vkH, 'i': vkI,
	'j': vkJ, 'k': vkK, 'l': vkL, 'm': vkM, 'n': vkN, 'o': vkO, 'p': vkP, 'q': vkQ, 'r': vkR,
	's': vkS, 't': vkT, 'u': vkU, 'v': vkV, 'w': vkW, 'x': vkX, 'y': vkY, 'z': vkZ,
	'0': vk0, '1': vk1, '2': vk2, '3': vk3, '4': vk4, '5': vk5, '6': vk6, '7': vk7, '8': vk8, '9': vk9,
	'-': vkMinus, '=': vkEqual, '[': vkLeftBracket, ']': vkRightBracket, '\\': vkBackslash,
	';': vkSemicolon, '\'': vkQuote, ',': vkComma, '.': vkPeriod, '/': vkSlash, '`': vkGrave,
	' ': vkSpace, '\n': vkReturn, '\t': vkTab,
}

// shiftedKeys maps the shifted US punctuation back to its base key.
var shiftedKeys = map[rune]rune{
	'!': '1', '@': '2', '#': '3', '$': '4', '%': '5', '^': '6', '&': '7', '*': '8', '(': '9', ')': '0',
	'_': '-', '+': '=', '{': '[', '}': ']', '|': '\\', ':': ';', '"': '\'', '<': ',', '>': '.', '?': '/', '~': '`',
}

// keyCode resolves one token of a combo like "cmd+shift+t".
func keyCode(token string) (comboKey, error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if k, ok := modifierKeys[t]; ok {
		return k, nil
	}
	if code, ok := namedKeys[t]; ok {
		return comboKey{code: code}, nil
	}
	if len(t) >= 2 && t[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(t, "f%d", &n); err == nil && n >= 1 && n <= len(fnKeys) {
			return comboKey{code: fnKeys[n-1]}, nil
		}
	}
	runes := []rune(strings.TrimSpace(token))
	if len(runes) == 1 {
		r := runes[0]
		shift := false
		if unicode.IsUpper(r) {
			r, shift = unicode.ToLower(r), true
		} else if base, ok := shiftedKeys[r]; ok {
			r, shift = base, true
		}
		if code, ok := charKeys[r]; ok {
			return comboKey{code: code, shift: shift}, nil
		}
		return comboKey{}, fmt.Errorf("no key for %q on a US layout", token)
	}
	return comboKey{}, fmt.Errorf("unknown key %q", token)
}

// comboKeys splits a combo into its tokens in press order. A shifted character adds
// an implicit shift modifier just before it.
func comboKeys(combo string) ([]comboKey, error) {
	parts := strings.Split(combo, "+")
	if strings.HasSuffix(combo, "++") { // literal plus as the last key
		parts = append(strings.Split(strings.TrimSuffix(combo, "++"), "+"), "+")
	}
	var keys []comboKey
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		k, err := keyCode(part)
		if err != nil {
			return nil, err
		}
		if k.shift {
			keys = append(keys, modifierKeys["shift"])
			k.shift = false
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("empty key combo")
	}
	return keys, nil
}

// pressCombo posts key downs in order and ups in reverse. Every event carries the
// modifier flags in force at that moment, which is what apps actually read; the
// modifier key events themselves are for apps that watch the keys directly.
func pressCombo(keys []comboKey) {
	flags := heldFlags
	for _, k := range keys {
		flags |= k.flag
		C.clawPostKey(C.uint16_t(k.code), 1, C.uint64_t(flags))
	}
	for i := len(keys) - 1; i >= 0; i-- {
		// A modifier's up event carries the flags as they are once it is released.
		flags &^= keys[i].flag
		flags |= heldFlags
		C.clawPostKey(C.uint16_t(keys[i].code), 0, C.uint64_t(flags))
	}
}

func pressKeys(combo string) error {
	keys, err := comboKeys(combo)
	if err != nil {
		return err
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	pressCombo(keys)
	return nil
}

func holdDown(keys []comboKey) {
	for _, k := range keys {
		heldFlags |= k.flag
		C.clawPostKey(C.uint16_t(k.code), 1, C.uint64_t(heldFlags))
	}
}

func holdUp(keys []comboKey) {
	for i := len(keys) - 1; i >= 0; i-- {
		heldFlags &^= keys[i].flag
		C.clawPostKey(C.uint16_t(keys[i].code), 0, C.uint64_t(heldFlags))
	}
}

// holdKeysBegin presses every key in combo and leaves them down; holdKeysEnd releases
// them in reverse. Used to hold modifiers around a pointer action.
func holdKeysBegin(combo string) error {
	keys, err := comboKeys(combo)
	if err != nil {
		return err
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	holdDown(keys)
	return nil
}

func holdKeysEnd(combo string) {
	keys, err := comboKeys(combo)
	if err != nil {
		return
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	holdUp(keys)
}

func holdKeys(combo string, d time.Duration) error {
	keys, err := comboKeys(combo)
	if err != nil {
		return err
	}
	inputMu.Lock()
	defer inputMu.Unlock()
	holdDown(keys)
	time.Sleep(d)
	holdUp(keys)
	return nil
}

// screenGeometry reports the main display in global display points, the space
// CGEvent mouse coordinates use. A Retina screenshot is twice this size; the caller
// scales by the ratio, so that works out.
func screenGeometry() (x, y, w, h int) {
	var cx, cy, cw, ch C.double
	C.clawMainDisplayBounds(&cx, &cy, &cw, &ch)
	return int(cx), int(cy), int(cw), int(ch)
}

// showNotification uses Notification Center through osascript.
func showNotification(title, body string) error {
	esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "default"`, esc(body), esc(title))
	return exec.Command("osascript", "-e", script).Run()
}
