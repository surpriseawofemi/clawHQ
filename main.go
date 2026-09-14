package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// Events pushed to the frontend. Registering them gives the binding generator typed
// JS/TS signatures.
func init() {
	application.RegisterEvent[gateway.Event]("gateway:event")
	application.RegisterEvent[gateway.Status]("gateway:status")
	application.RegisterEvent[node.Status]("node:status")
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
		},
		// Unimplemented commands are logged in full so the surface can be extended
		// against real gateway traffic instead of a guessed schema.
		func(command, params string) {
			log.Printf("node: unhandled command %q params=%s", command, params)
		},
	)

	// Load the shared-folder list up front so the settings panel reflects what is
	// configured even while the node role is switched off.
	nodeHost.SetSharedFolders(cfgStore.Read().Node.SharedFolders)

	app = application.New(application.Options{
		Name:        "ClawHQ",
		Description: "Desktop command center for your OpenClaw agent org",
		Services: []application.Service{
			application.NewService(&GatewayService{conn: conn, store: cfgStore}),
			application.NewService(&ConfigService{store: cfgStore}),
			application.NewService(&DaemonService{}),
			application.NewService(&NodeService{host: nodeHost, store: cfgStore, conn: conn}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
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

	// Best-effort auto-connect to the last used gateway: a paired install should come
	// up connected without the user opening settings.
	go func() {
		cfg := cfgStore.Read()
		profile, ok := cfg.ActiveGateway()
		if !ok || !conn.HasStoredPairing(profile.ID) {
			return
		}
		if _, err := conn.Connect(context.Background(), profile.ID, profile.URL, gateway.Credential{}); err != nil {
			// The UI surfaces this through connection status; nothing to do here.
			log.Printf("auto-connect failed: %v", err)
			return
		}

		// Bring the node role back up too, if the user left it on.
		if cfg.Node.Enabled {
			nodeHost.SetSharedFolders(cfg.Node.SharedFolders)
			if _, err := nodeHost.Start(context.Background(), profile.ID, profile.URL, ""); err != nil {
				log.Printf("node auto-start failed: %v", err)
			}
		}
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
