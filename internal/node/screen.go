package node

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"time"
)

// CmdScreenSnapshot is OpenClaw's own screen-capture command. Unlike an invented name
// it sits in the gateway's allowlist, so a node advertising it is actually reachable.
const CmdScreenSnapshot = "screen.snapshot"

// screenSnapshot answers a screen.snapshot invoke.
//
// Params:  {screenIndex, maxWidth, format}
// Payload: {base64, format, width, height, screenIndex}
func (h *Host) screenSnapshot(raw json.RawMessage) (any, string) {
	var params struct {
		ScreenIndex int    `json:"screenIndex"`
		MaxWidth    int    `json:"maxWidth"`
		Format      string `json:"format"`
	}
	_ = json.Unmarshal(raw, &params)

	img, err := captureScreen(params.ScreenIndex)
	if err != nil {
		return nil, err.Error()
	}
	fullW, fullH := img.Bounds().Dx(), img.Bounds().Dy()

	if params.MaxWidth > 0 && img.Bounds().Dx() > params.MaxWidth {
		img = downscale(img, params.MaxWidth)
	}

	// computer.act coordinates are pixels in the most recent screenshot, so record
	// how this one maps back onto the screen.
	sx, sy, sw, sh := screenGeometry()
	if sw == 0 || sh == 0 {
		sx, sy, sw, sh = 0, 0, fullW, fullH
	}
	frameID := fmt.Sprintf("%d", time.Now().UnixNano())
	h.rememberFrame(frameGeometry{
		imgW: img.Bounds().Dx(), imgH: img.Bounds().Dy(),
		screenX: sx, screenY: sy, screenW: sw, screenH: sh,
		frameID: frameID,
	})

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err.Error()
	}

	return map[string]any{
		"base64":      base64.StdEncoding.EncodeToString(buf.Bytes()),
		"format":      "png",
		"width":       img.Bounds().Dx(),
		"height":      img.Bounds().Dy(),
		"screenIndex": params.ScreenIndex,
		"frameId":     frameID,
	}, ""
}

// downscale does a nearest-neighbour resize to maxWidth.
//
// Good enough for a desktop preview and keeps the binary dependency-free; a smoother
// filter would mean pulling in golang.org/x/image for no visible gain at these sizes.
func downscale(src *image.RGBA, maxWidth int) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dw := maxWidth
	dh := sh * dw / sw
	if dh < 1 {
		dh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := y * sh / dh
		for x := 0; x < dw; x++ {
			sx := x * sw / dw
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
