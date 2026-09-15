package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is handed to the OS at startup so the dock, Cmd-Tab switcher and About
// box show the ClawHQ mark even when the bundle's own icon lookup does not apply,
// such as a bare `bin/ClawHQ` run or a dev build.
//
//go:embed build/appicon.png
var appIcon []byte

// version is the running release, kept in a file rather than injected with
// -ldflags because the Wails taskfiles hardcode their link flags. CI asserts that
// this matches the tag being built, so the updater can never mistake which
// version it is.
//
//go:embed VERSION
var versionFile string

// menuBarRef holds the menu bar once the app exists; node status arrives earlier.
var menuBarRef atomic.Value

// Events pushed to the frontend. Registering them gives the binding generator typed
// JS/TS signatures.
func init() {
	application.RegisterEvent[gateway.Event]("gateway:event")
	application.RegisterEvent[gateway.Status]("gateway:status")
	application.RegisterEvent[node.Status]("node:status")
	application.RegisterEvent[AgentNotification]("node:notify")
	application.RegisterEvent[node.ExecRequest]("node:exec-request")
	application.RegisterEvent[DesktopSelect]("desktop:select")
}

// identityDir is where the Ed25519 device identity and device token live. It is
// deliberately outside the project so moving or re-cloning the source never costs the
// pairing.
func identityDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return ".clawhq"
		}
		base = home
	}
	return filepath.Join(base, "ClawHQ", "identity")
}

func main() {
	cfgStore, err := store.New()
	if err != nil {
		log.Fatal(err)
	}

	// The connection is created before the app so services can hold it; the emit
	// callbacks close over app, which is assigned just below.
	var app *application.App

	// The update service needs the App itself, which does not exist until services
	// are already being registered, so hold the pointer and fill it in afterwards.
	updateSvc := &UpdateService{store: cfgStore}
	windowSvc := &WindowService{}
	var notify *notifier

	conn, err := gateway.New(
		identityDir(),
		func(ev gateway.Event) {
			if app != nil {
				app.Event.Emit("gateway:event", ev)
			}
		},
		func(st gateway.Status) {
			if app != nil {
				app.Event.Emit("gateway:status", st)
			}
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	nodeHost := node.New(
		identityDir(),
		cfgStore,
		func(st node.Status) {
			if app != nil {
				app.Event.Emit("node:status", st)
			}
			if bar, ok := menuBarRef.Load().(*menuBar); ok && bar != nil {
				bar.setNode(st)
			}
		},
		// system.notify from an agent: an OS notification from ClawHQ plus a banner,
		// both naming the agent. The notifier is wired just below.
		func(n node.Notification) {
			if notify != nil {
				notify.deliver(n)
			}
		},
		// Unimplemented commands are logged in full so the surface can be extended
		// against real gateway traffic instead of a guessed schema.
		func(command, params string) {
			log.Printf("node: unhandled command %q params=%s", command, params)
		},
	)

	// Load the node settings up front so the settings panel reflects what is
	// configured even while the role is still connecting.
	startCfg := cfgStore.Read()
	nodeHost.SetSharedFolders(startCfg.Node.SharedFolders)
	nodeHost.SetDesktopControl(startCfg.Node.DesktopControl)
	nodeHost.SetExecPolicy(startCfg.Node.Exec.Mode, startCfg.Node.Exec.Allow, startCfg.Node.Exec.Agents)

	// OS notifications go through the platform's own centre under ClawHQ's name and
	// icon, rather than through a script runner that gets the credit.
	nativeNotifications := notifications.New()
	inbox, err := store.NewInbox()
	if err != nil {
		log.Fatal(err)
	}
	execLog, err := store.NewExecLog()
	if err != nil {
		log.Fatal(err)
	}
	notify = newNotifier(conn, nodeHost, nativeNotifications, inbox)

	// The autopilot starts, pairs and approves the node role whenever the operator
	// connection is up, so nothing below has to think about it.
	auto := newNodeAutopilot(conn, nodeHost, cfgStore)
	nodeHost.SetHooks(node.Hooks{
		OnPending:      auto.approvePairing,
		OnConnected:    auto.verifySurface,
		OnStalePairing: auto.forgetNode,
		LocalToken:     localGatewayToken,
		// A command waiting on the user: the UI shows it as a banner.
		OnExecRequest: func(req node.ExecRequest) {
			if app != nil {
				app.Event.Emit("node:exec-request", req)
			}
		},
		// Every command, ran or refused, goes to the local audit log.
		OnExecRecord: func(rec node.ExecRecord) {
			if _, err := execLog.Append(store.ExecRecord(rec)); err != nil {
				log.Printf("exec log: %v", err)
			}
		},
		OnAllowAlways: func(commandText string) {
			if cfg, err := cfgStore.AllowExecCommand(commandText); err == nil {
				nodeHost.SetExecPolicy(cfg.Node.Exec.Mode, cfg.Node.Exec.Allow, cfg.Node.Exec.Agents)
			}
		},
		OnTrustAgent: func(agentID string) {
			if cfg, err := cfgStore.SetAgentExecMode(agentID, store.ExecAllow); err == nil {
				nodeHost.SetExecPolicy(cfg.Node.Exec.Mode, cfg.Node.Exec.Allow, cfg.Node.Exec.Agents)
			}
		},
		OnExecModeChanged: func(mode string) {
			if cfg, err := cfgStore.UpdateNode(func(n *store.NodeConfig) { n.Exec.Mode = mode }); err == nil {
				nodeHost.SetExecPolicy(cfg.Node.Exec.Mode, cfg.Node.Exec.Allow, cfg.Node.Exec.Agents)
			}
		},
	})

	configSvc := &ConfigService{store: cfgStore}
	loginSvc := &LoginService{}

	app = application.New(application.Options{
		Name:        "ClawHQ",
		Description: "Desktop command center for your OpenClaw agent org",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(&GatewayService{conn: conn, store: cfgStore}),
			application.NewService(configSvc),
			application.NewService(loginSvc),
			application.NewService(&DaemonService{}),
			application.NewService(&NodeService{host: nodeHost, store: cfgStore, conn: conn, auto: auto}),
			application.NewService(updateSvc),
			application.NewService(windowSvc),
			application.NewService(nativeNotifications),
			application.NewService(&InboxService{inbox: inbox}),
			application.NewService(&ExecLogService{log: execLog}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// The menu bar decides: closing the window hides it while "keep running"
			// is on and quits otherwise (see menubar.go).
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	mainWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      mainWindowName,
		Title:     "ClawHQ",
		Width:     1280,
		Height:    840,
		MinWidth:  940,
		MinHeight: 600,
		Mac: application.MacWindow{
			// Matches the Electron build: traffic lights kept, title bar dropped so the
			// sidebar runs to the top edge.
			InvisibleTitleBarHeight: 52,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(14, 16, 21),
		URL:              "/",
	})

	// Auto-connect to the last used gateway: a paired install should come up
	// connected without the user opening settings. A gateway that is down at launch
	// is retried with backoff; the reconnect watcher takes over once a connection
	// has been made. The autopilot brings the node role up when this lands.
	go func() {
		cfg := cfgStore.Read()
		profile, ok := cfg.ActiveGateway()
		if !ok || !cfg.AutoConnect || !conn.HasStoredPairing(profile.ID) {
			return
		}
		delay := 5 * time.Second
		for {
			if !cfgStore.Read().AutoConnect {
				return
			}
			st, err := conn.Connect(context.Background(), profile.ID, profile.URL, gateway.Credential{})
			if err == nil && st.Phase == gateway.PhaseConnected {
				return
			}
			if err != nil {
				log.Printf("auto-connect failed: %v", err)
			}
			// A user action (connect, remove, re-pair) supersedes this loop.
			if cur := conn.Status(); cur.Phase == gateway.PhaseConnected || cur.GatewayID != profile.ID && cur.Phase != gateway.PhaseIdle {
				return
			}
			time.Sleep(delay)
			if delay < 60*time.Second {
				delay *= 2
			}
		}
	}()

	updateSvc.app = app
	windowSvc.app = app
	notify.app = app

	// Menu bar presence: the node keeps serving after the window is closed.
	bar := newMenuBar(app, cfgStore, loginSvc, updateSvc, windowSvc)
	bar.hideOnClose(mainWin)
	bar.setNode(nodeHost.Status())
	menuBarRef.Store(bar)
	configSvc.onPrefsChanged = bar.sync

	// Self-update from GitHub releases. Failing to wire this up is not fatal;
	// the app simply will not offer updates.
	if err := initUpdater(app, strings.TrimSpace(versionFile)); err != nil {
		log.Printf("updater unavailable: %v", err)
	} else {
		go updateSvc.runLoop()
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
