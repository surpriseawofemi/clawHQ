//go:build !(darwin && cgo)

package main

import "github.com/wailsapp/wails/v3/pkg/application"

func tuneWindowForResize(_ *application.WebviewWindow, _, _, _ uint8) {}
