package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// Repository is the update source. Public, so no token is needed; that only
// raises the anonymous rate limit from 60 to 5000 requests an hour.
const updateRepository = "surpriseawofemi/clawHQ"

// checkInterval is how often the background check runs. Releases go out several
// times a day while the app is being built up, so an hour keeps every machine
// close to current without hammering GitHub.
const checkInterval = time.Hour

// UpdateStatus is what the UI renders.
type UpdateStatus struct {
	CurrentVersion string `json:"currentVersion"`
	State          string `json:"state"`
	Available      bool   `json:"available"`
	LatestVersion  string `json:"latestVersion"`
	Notes          string `json:"notes"`
	Error          string `json:"error"`
	// AutoUpdate mirrors the setting so the panel has one source of truth.
	AutoUpdate bool `json:"autoUpdate"`
	// LastCheckedAtMs is when the background loop or Check last asked GitHub.
	LastCheckedAtMs int64 `json:"lastCheckedAtMs"`
}

// UpdateService exposes the updater to the frontend.
type UpdateService struct {
	app   *application.App
	store *store.Store

	mu          sync.Mutex
	lastChecked time.Time
	installing  bool
}

func (s *UpdateService) Status() UpdateStatus {
	u := s.app.Updater
	s.mu.Lock()
	last := s.lastChecked
	s.mu.Unlock()
	var lastMs int64
	if !last.IsZero() {
		lastMs = last.UnixMilli()
	}
	return UpdateStatus{
		CurrentVersion:  u.CurrentVersion(),
		State:           string(u.State()),
		AutoUpdate:      s.store.Read().AutoUpdate,
		LastCheckedAtMs: lastMs,
	}
}

// SetAutoUpdate switches automatic installs on or off. Switching it on runs a
// check right away, so a waiting update is not left until the next hour.
func (s *UpdateService) SetAutoUpdate(ctx context.Context, on bool) (UpdateStatus, error) {
	if _, err := s.store.SetAutoUpdate(on); err != nil {
		return s.Status(), err
	}
	if on {
		go s.checkAndMaybeInstall(context.Background())
	}
	return s.Status(), nil
}

// runLoop checks on the hour and installs when the setting says so. The first
// check waits a minute so launch is not competing with GitHub for the network.
func (s *UpdateService) runLoop() {
	time.Sleep(time.Minute)
	for {
		s.checkAndMaybeInstall(context.Background())
		time.Sleep(checkInterval)
	}
}

// checkAndMaybeInstall is one background pass: check, and install if allowed.
func (s *UpdateService) checkAndMaybeInstall(ctx context.Context) {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	status, err := s.Check(checkCtx)
	cancel()
	if err != nil {
		log.Printf("update check: %v", err)
		return
	}
	if !status.Available || !s.store.Read().AutoUpdate {
		return
	}
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return
	}
	s.installing = true
	s.mu.Unlock()
	log.Printf("update: installing %s automatically", status.LatestVersion)
	installCtx, cancelInstall := context.WithTimeout(ctx, 15*time.Minute)
	defer cancelInstall()
	if err := s.Install(installCtx); err != nil {
		log.Printf("update install: %v", err)
		s.mu.Lock()
		s.installing = false
		s.mu.Unlock()
	}
}

// Check asks GitHub whether a newer release exists. A nil release means the app
// is current, which is not an error.
func (s *UpdateService) Check(ctx context.Context) (UpdateStatus, error) {
	s.mu.Lock()
	s.lastChecked = time.Now()
	s.mu.Unlock()
	status := s.Status()
	rel, err := s.app.Updater.Check(ctx)
	if err != nil {
		status.Error = err.Error()
		return status, err
	}
	if rel == nil {
		return status, nil
	}
	status.Available = true
	status.LatestVersion = rel.Version
	status.Notes = rel.Notes
	return status, nil
}

// Install downloads the update, swaps it in, and relaunches.
//
// On macOS the downloaded artifact is a whole signed .app bundle and the updater
// replaces the bundle wholesale, so the signature survives the swap. Nothing is
// re-signed and nothing is quarantined: the download comes over Go's HTTP client,
// not a browser, so Gatekeeper never tags it.
func (s *UpdateService) Install(ctx context.Context) error {
	if err := s.app.Updater.DownloadAndInstall(ctx); err != nil {
		return fmt.Errorf("install update: %w", err)
	}
	return s.app.Updater.Restart(ctx)
}

// initUpdater wires the updater to GitHub releases. A failure here is logged and
// swallowed: not being able to self-update is not a reason to refuse to start.
func initUpdater(app *application.App, currentVersion string) error {
	provider, err := github.New(github.Config{Repository: updateRepository})
	if err != nil {
		return fmt.Errorf("github update provider: %w", err)
	}
	// No CheckInterval here: UpdateService.runLoop does the polling so it can also
	// install when the setting allows, which the built-in loop cannot.
	return app.Updater.Init(updater.Config{
		// The tag is "v0.1.2"; the updater compares bare semver.
		CurrentVersion: strings.TrimPrefix(currentVersion, "v"),
		Providers:      []updater.Provider{provider},
	})
}
