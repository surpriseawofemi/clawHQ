package main

import (
	"net/url"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// desktopWindowName is the Wails window name for the detached desktop viewer.
const desktopWindowName = "desktop"

// DesktopSelect asks an open desktop window to switch to a node.
type DesktopSelect struct {
	NodeID string `json:"nodeId"`
}

// WindowService opens the desktop viewer as its own native window, so a remote
// screen can sit beside the chat, on another display, or float above everything.
type WindowService struct {
	app *application.App
}

// OpenDesktop shows the desktop window, creating it on first use, and points it at
// the given node. An empty id keeps whatever the window already shows.
func (s *WindowService) OpenDesktop(nodeID string) {
	if s.app == nil {
		return
	}
	if win, ok := s.app.Window.Get(desktopWindowName); ok {
		win.Show()
		win.Focus()
		if nodeID != "" {
			s.app.Event.Emit("desktop:select", DesktopSelect{NodeID: nodeID})
		}
		return
	}
	target := "/?view=desktop"
	if nodeID != "" {
		target += "&node=" + url.QueryEscape(nodeID)
	}
	s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             desktopWindowName,
		Title:            "Desktop",
		Width:            920,
		Height:           640,
		MinWidth:         480,
		MinHeight:        320,
		BackgroundColour: application.NewRGB(14, 16, 21),
		URL:              target,
	})
}

// SetDesktopAlwaysOnTop pins the desktop window above other apps' windows.
func (s *WindowService) SetDesktopAlwaysOnTop(on bool) {
	if s.app == nil {
		return
	}
	if win, ok := s.app.Window.Get(desktopWindowName); ok {
		win.SetAlwaysOnTop(on)
	}
}

// CloseDesktop closes the desktop window if it is open.
func (s *WindowService) CloseDesktop() {
	if s.app == nil {
		return
	}
	if win, ok := s.app.Window.Get(desktopWindowName); ok {
		win.Close()
	}
}
