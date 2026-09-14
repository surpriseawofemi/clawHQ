package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// Repository is the update source. Public, so no token is needed; that only
// raises the anonymous rate limit from 60 to 5000 requests an hour.
const updateRepository = "surpriseawofemi/clawHQ"

// checkInterval is deliberately unhurried. This is a desktop app someone leaves
// open for days, not a service that needs the newest build within minutes.
const checkInterval = 6 * time.Hour

// UpdateStatus is what the UI renders.
type UpdateStatus struct {
	CurrentVersion  string `json:"currentVersion"`
	State           string `json:"state"`
	Available       bool   `json:"available"`
	LatestVersion   string `json:"latestVersion"`
	Notes           string `json:"notes"`
	Error           string `json:"error"`
}

// UpdateService exposes the updater to the frontend.
type UpdateService struct {
	app *application.App
}

func (s *UpdateService) Status() UpdateStatus {
	u := s.app.Updater
	return UpdateStatus{
		CurrentVersion: u.CurrentVersion(),
		State:          string(u.State()),
	}
}

// Check asks GitHub whether a newer release exists. A nil release means the app
// is current, which is not an error.
func (s *UpdateService) Check(ctx context.Context) (UpdateStatus, error) {
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
	return app.Updater.Init(updater.Config{
		// The tag is "v0.1.2"; the updater compares bare semver.
		CurrentVersion: strings.TrimPrefix(currentVersion, "v"),
		Providers:      []updater.Provider{provider},
		CheckInterval:  checkInterval,
	})
}
