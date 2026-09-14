package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Attachments.
//
// A 2026.9 gateway keeps image attachments and silently drops every other kind
// (openclaw/openclaw#48123), so files are delivered the one way that is known to
// reach the model: text goes inline in the message as fenced blocks, images go as
// `{type:"image"}` attachments, and anything else is reported back to the user as
// not sendable rather than vanishing without a trace.

// Attachment describes one thing the user picked for the composer.
type Attachment struct {
	Path string `json:"path"`
	Name string `json:"name"`
	// Kind is text, image, folder, or unsupported.
	Kind string `json:"kind"`
	Size int64  `json:"size"`
	// Note explains a skip or a limit in one line.
	Note string `json:"note,omitempty"`
}

const (
	maxTextFileBytes  = 256 * 1024
	maxInlineBytes    = 512 * 1024 // total text across a message
	maxImageBytes     = 8 * 1024 * 1024
	maxFolderFiles    = 80
	previewSniffBytes = 8 * 1024
)

var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true, ".next": true,
	"vendor": true, "target": true, "__pycache__": true, ".venv": true, "venv": true,
	".idea": true, ".vscode": true, "bin": true, "obj": true, ".cache": true,
}

var imageMimes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp",
}

// PickFiles opens the native file chooser and describes what was chosen.
func (s *GatewayService) PickFiles() ([]Attachment, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("no application")
	}
	paths, err := app.Dialog.OpenFile().
		SetTitle("Attach files").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}
	return describeAll(paths), nil
}

// PickFolder opens the native folder chooser.
func (s *GatewayService) PickFolder() ([]Attachment, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("no application")
	}
	path, err := app.Dialog.OpenFile().
		SetTitle("Attach a folder").
		CanChooseFiles(false).
		CanChooseDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil
	}
	return describeAll([]string{path}), nil
}

func describeAll(paths []string) []Attachment {
	out := make([]Attachment, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, describe(p))
	}
	return out
}

func describe(path string) Attachment {
	a := Attachment{Path: path, Name: filepath.Base(path)}
	info, err := os.Stat(path)
	if err != nil {
		a.Kind = "unsupported"
		a.Note = err.Error()
		return a
	}
	if info.IsDir() {
		a.Kind = "folder"
		n, total, _ := walkFolder(path, nil)
		a.Size = total
		a.Note = fmt.Sprintf("%d text files", n)
		return a
	}
	a.Size = info.Size()
	switch classify(path) {
	case "image":
		a.Kind = "image"
		if a.Size > maxImageBytes {
			a.Kind, a.Note = "unsupported", "image is over 8 MB"
		}
	case "text":
		a.Kind = "text"
		if a.Size > maxTextFileBytes {
			a.Kind, a.Note = "unsupported", "text file is over 256 KB"
		}
	default:
		a.Kind, a.Note = "unsupported", "only text files, images and folders can be sent"
	}
	return a
}

// classify decides how a file travels: image, text, or neither. Extension first,
// then a sniff for NUL bytes and valid UTF-8, so a Makefile or an extensionless
// script still counts as text.
func classify(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := imageMimes[ext]; ok {
		return "image"
	}
	f, err := os.Open(path)
	if err != nil {
		return "binary"
	}
	defer f.Close()
	buf := make([]byte, previewSniffBytes)
	n, _ := f.Read(buf)
	buf = buf[:n]
	if n == 0 {
		return "text"
	}
	for _, b := range buf {
		if b == 0 {
			return "binary"
		}
	}
	// A cut-off multibyte sequence at the sniff boundary is fine; anything else that
	// fails UTF-8 is treated as binary.
	if !utf8.Valid(buf) && !utf8.Valid(buf[:n-utf8.UTFMax]) {
		return "binary"
	}
	return "text"
}

// walkFolder visits text files under root in a stable order, skipping build and VCS
// directories, and stops at the file cap. visit may be nil to only count.
func walkFolder(root string, visit func(path string, size int64) error) (count int, total int64, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && (skippedDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil || info.Size() > maxTextFileBytes || classify(path) != "text" {
			return nil
		}
		if count >= maxFolderFiles {
			return fs.SkipAll
		}
		count++
		total += info.Size()
		if visit != nil {
			return visit(path, info.Size())
		}
		return nil
	})
	return count, total, err
}

// SendChatWithFiles sends a message with the given paths attached, following the
// delivery rules above. The returned raw is the gateway's chat.send reply.
func (s *GatewayService) SendChatWithFiles(ctx context.Context, sessionKey, message string, paths []string) (string, error) {
	var (
		blocks      []string
		images      []map[string]any
		skipped     []string
		inlineBytes int64
	)

	addText := func(path, display string) {
		if inlineBytes >= maxInlineBytes {
			skipped = append(skipped, display+" (message size limit)")
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			skipped = append(skipped, display+" ("+err.Error()+")")
			return
		}
		inlineBytes += int64(len(data))
		lang := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		fence := "```"
		for strings.Contains(string(data), fence) {
			fence += "`"
		}
		blocks = append(blocks, fmt.Sprintf("%s\n%s%s\n%s\n%s", display, fence, lang, strings.TrimRight(string(data), "\n"), fence))
	}

	for _, p := range paths {
		a := describe(p)
		switch a.Kind {
		case "text":
			addText(p, "File: "+p)
		case "image":
			data, err := os.ReadFile(p)
			if err != nil {
				skipped = append(skipped, a.Name+" ("+err.Error()+")")
				continue
			}
			images = append(images, map[string]any{
				"type":     "image",
				"name":     a.Name,
				"fileName": a.Name,
				"mimeType": mimeFor(p),
				"content":  base64.StdEncoding.EncodeToString(data),
			})
		case "folder":
			var files []string
			_, _, _ = walkFolder(p, func(path string, _ int64) error {
				files = append(files, path)
				return nil
			})
			sort.Strings(files)
			blocks = append(blocks, fmt.Sprintf("Folder: %s (%d text files attached below; binaries, dotfiles and build directories skipped)", p, len(files)))
			for _, f := range files {
				rel, _ := filepath.Rel(p, f)
				addText(f, "File: "+filepath.ToSlash(filepath.Join(filepath.Base(p), rel)))
			}
		default:
			skipped = append(skipped, a.Name+" ("+a.Note+")")
		}
	}

	full := strings.TrimSpace(message)
	if len(blocks) > 0 {
		if full != "" {
			full += "\n\n"
		}
		full += strings.Join(blocks, "\n\n")
	}
	if full == "" && len(images) == 0 {
		if len(skipped) > 0 {
			return "", fmt.Errorf("nothing sendable: %s", strings.Join(skipped, "; "))
		}
		return "", fmt.Errorf("nothing to send")
	}

	var attachments json.RawMessage
	if len(images) > 0 {
		encoded, err := json.Marshal(images)
		if err != nil {
			return "", err
		}
		attachments = encoded
	}

	raw, err := s.conn.SendChatWith(ctx, sessionKey, full, attachments)
	if err != nil {
		return "", err
	}
	// Surface skips alongside the reply so the UI can tell the user.
	var reply map[string]any
	_ = json.Unmarshal(raw, &reply)
	if reply == nil {
		reply = map[string]any{}
	}
	if len(skipped) > 0 {
		reply["skipped"] = skipped
	}
	out, _ := json.Marshal(reply)
	return string(out), nil
}

func mimeFor(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if m, ok := imageMimes[ext]; ok {
		return m
	}
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	return "application/octet-stream"
}
