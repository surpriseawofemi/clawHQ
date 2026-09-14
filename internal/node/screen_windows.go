//go:build windows

package node

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

// Windows screen capture via GDI. Pure syscall so the build stays CGO-free, which is
// what keeps ClawHQ a single self-contained binary.
var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBM = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000 // include layered windows, or overlays come out blank
	dibRGBColors = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

func systemMetric(index int32) int32 {
	v, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(v)
}

// captureScreen grabs the whole virtual desktop, which is what a viewer expects on a
// multi-monitor machine. screenIndex is accepted for protocol compatibility but only
// index 0 (the full virtual desktop) is implemented.
func captureScreen(screenIndex int) (*image.RGBA, error) {
	if screenIndex != 0 {
		return nil, fmt.Errorf("only screenIndex 0 is supported on windows")
	}

	x := systemMetric(smXVirtualScreen)
	y := systemMetric(smYVirtualScreen)
	width := systemMetric(smCXVirtualScreen)
	height := systemMetric(smCYVirtualScreen)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("could not determine screen size")
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	bitmap, _, _ := procCreateCompatibleBM.Call(screenDC, uintptr(width), uintptr(height))
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(bitmap)

	old, _, _ := procSelectObject.Call(memDC, bitmap)
	defer procSelectObject.Call(memDC, old)

	ok, _, _ := procBitBlt.Call(
		memDC, 0, 0, uintptr(width), uintptr(height),
		screenDC, uintptr(x), uintptr(y), srcCopy|captureBlt,
	)
	if ok == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	// Negative height requests a top-down DIB, so rows arrive in image order.
	info := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       width,
		Height:      -height,
		Planes:      1,
		BitCount:    32,
		Compression: 0,
	}}

	buf := make([]byte, int(width)*int(height)*4)
	res, _, _ := procGetDIBits.Call(
		memDC, bitmap, 0, uintptr(height),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&info)),
		dibRGBColors,
	)
	if res == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	// GDI hands back BGRA with an unused alpha byte; swap to RGBA and force opaque.
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i+0] = buf[i+2]
		img.Pix[i+1] = buf[i+1]
		img.Pix[i+2] = buf[i+0]
		img.Pix[i+3] = 0xFF
	}
	return img, nil
}

func screenCaptureSupported() bool { return true }
