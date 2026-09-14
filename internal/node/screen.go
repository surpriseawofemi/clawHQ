package node

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
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

	if params.MaxWidth > 0 && img.Bounds().Dx() > params.MaxWidth {
		img = downscale(img, params.MaxWidth)
	}

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
