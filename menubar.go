package main

import (
	"context"
	_ "embed"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/surpriseawofemi/clawhq/internal/store"
)

// mainWindowName is the Wails window name of the command centre itself.
const mainWindowName = "main"

//go:embed build/trayicon.png
var trayIcon []byte

// menuBar keeps ClawHQ present after its window is closed: an icon in the menu bar
// (system tray elsewhere) with the node's state, a way back to the window, and the
// launch-at-login switch. The node role only helps while ClawHQ runs, so closing the
// window hides it instead of quitting while this is on.
type menuBar struct {
	app      *application.App
	store    *store.Store
	login    *LoginService
	update   *UpdateService
	windows  *WindowService
	mu       sync.Mutex
	tray     *application.SystemTray
	menu     *application.Menu
	nodeItem *application.MenuItem
	loginOn  *application.MenuItem
	keepOn   *application.MenuItem
	nodeLine string
}

func newMenuBar(app *application.App, st *store.Store, login *LoginService, update *UpdateService, windows *WindowService) *menuBar {
	m := &menuBar{app: app, store: st, login: login, update: update, windows: windows, nodeLine: "Node: off"}
	m.tray = app.SystemTray.New()
	m.tray.SetIcon(trayIcon)
	m.tray.SetTooltip("ClawHQ")
	m.menu = app.NewMenu()
	m.build()
	m.tray.SetMenu(m.menu)
	m.tray.OnClick(func() { m.showMain() })
	return m
}

func (m *menuBar) build() {
	m.menu.Add("Open ClawHQ").OnClick(func(*application.Context) { m.showMain() })
	m.nodeItem = m.menu.Add(m.nodeLine).SetEnabled(false)
	m.menu.Add("Open desktop viewer").OnClick(func(*application.Context) {
		m.windows.OpenDesktop("")
	})
	m.menu.AddSeparator()
	m.loginOn = m.menu.AddCheckbox("Launch at login", m.login.Status().Enabled).OnClick(func(ctx *application.Context) {
		st, _ := m.login.Set(ctx.ClickedMenuItem().Checked())
		m.loginOn.SetChecked(st.Enabled)
		m.menu.Update()
	})
	m.loginOn.SetEnabled(m.login.Status().Supported)
	m.keepOn = m.menu.AddCheckbox("Keep running when the window closes", m.store.Read().MenuBar).OnClick(func(ctx *application.Context) {
		if _, err := m.store.SetMenuBar(ctx.ClickedMenuItem().Checked()); err == nil {
			m.sync()
		}
	})
	m.menu.AddSeparator()
	m.menu.Add("Check for updates…").OnClick(func(*application.Context) {
		m.showMain()
		go func() { _, _ = m.update.Check(context.Background()) }()
	})
	m.menu.Add("Quit ClawHQ").OnClick(func(*application.Context) { m.app.Quit() })
}

// sync refreshes the checkboxes from stored state, for changes made in Settings.
func (m *menuBar) sync() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keepOn.SetChecked(m.store.Read().MenuBar)
	m.loginOn.SetChecked(m.login.Status().Enabled)
	m.menu.Update()
}

// setNode mirrors the node role's state into the menu.
func (m *menuBar) setNode(st node.Status) {
	line := "Node: off"
	switch {
	case !st.Enabled:
	case st.Connected:
		line = "Node: connected"
	case st.Pairing == "awaiting-approval":
		line = "Node: pairing…"
	case st.Pairing == "reconnecting", st.Pairing == "connecting":
		line = "Node: connecting…"
	default:
		line = "Node: waiting for the gateway"
	}
	if len(st.PendingExec) > 0 {
		line += " · a command is waiting for you"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if line == m.nodeLine {
		return
	}
	m.nodeLine = line
	m.nodeItem.SetLabel(line)
	m.menu.Update()
}

func (m *menuBar) showMain() {
	if win, ok := m.app.Window.Get(mainWindowName); ok {
		win.Show()
		win.Focus()
	}
}

// hideOnClose turns the main window's close into a hide while the menu bar is on,
// so the node keeps serving. With it off, closing quits as before.
func (m *menuBar) hideOnClose(win *application.WebviewWindow) {
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if m.store.Read().MenuBar {
			log.Printf("menu bar: window closed, staying in the menu bar")
			e.Cancel()
			win.Hide()
			return
		}
		// The app no longer quits by itself when its last window goes, so do it here.
		log.Printf("menu bar: off, quitting with the window")
		go m.app.Quit()
	})
}
