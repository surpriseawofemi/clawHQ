package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/store"
)

// MediaService fetches the images and files agents attach to their replies.
// The gateway keeps them as artifacts: artifacts.download over the authenticated
// WebSocket returns either the bytes inline or a short-lived ticketed URL on the
// gateway's HTTP side, which needs no credentials. The webview cannot add auth
// headers to an <img>, so the bytes come through here as a data URL.
type MediaService struct {
	conn  *gateway.Conn
	store *store.Store
	mu    sync.Mutex
	cache map[string]string
	size  int
}

const mediaCacheCap = 80 << 20 // bytes of data URLs kept in memory
const mediaMaxBytes = 40 << 20

var mediaHTTP = &http.Client{Timeout: 60 * time.Second}

// Fetch returns a data: URL for one artifact of a session.
func (s *MediaService) Fetch(ctx context.Context, artifactID, sessionKey string) (string, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return "", fmt.Errorf("artifact id is empty")
	}
	s.mu.Lock()
	if hit, ok := s.cache[artifactID]; ok {
		s.mu.Unlock()
		return hit, nil
	}
	s.mu.Unlock()

	params := map[string]any{"artifactId": artifactID}
	if sessionKey != "" {
		params["sessionKey"] = sessionKey
	}
	raw, err := s.conn.Request(ctx, "artifacts.download", params)
	if err != nil {
		return "", err
	}
	var res struct {
		Artifact struct {
			MimeType string `json:"mimeType"`
		} `json:"artifact"`
		Encoding string `json:"encoding"`
		Data     string `json:"data"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	mime := res.Artifact.MimeType
	if mime == "" {
		mime = "application/octet-stream"
	}
	var out string
	switch {
	case res.Data != "":
		out = "data:" + mime + ";base64," + res.Data
	case res.URL != "":
		b, ct, err := s.get(ctx, res.URL)
		if err != nil {
			return "", err
		}
		if ct != "" {
			mime = ct
		}
		out = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
	default:
		return "", fmt.Errorf("the gateway returned neither bytes nor a URL for %s", artifactID)
	}
	s.mu.Lock()
	if s.cache == nil {
		s.cache = map[string]string{}
	}
	if s.size+len(out) > mediaCacheCap {
		s.cache = map[string]string{}
		s.size = 0
	}
	s.cache[artifactID] = out
	s.size += len(out)
	s.mu.Unlock()
	return out, nil
}

// get resolves a ticketed relative URL against the active gateway and downloads it.
// The ticket in the URL is the credential; the reverse-proxy path prefix of the
// WebSocket address is kept, as the gateway docs require.
func (s *MediaService) get(ctx context.Context, rel string) ([]byte, string, error) {
	profile, ok := s.store.Read().ActiveGateway()
	if !ok {
		return nil, "", fmt.Errorf("no active gateway")
	}
	base := strings.TrimSpace(profile.URL)
	switch {
	case strings.HasPrefix(base, "wss://"):
		base = "https://" + strings.TrimPrefix(base, "wss://")
	case strings.HasPrefix(base, "ws://"):
		base = "http://" + strings.TrimPrefix(base, "ws://")
	}
	base = strings.TrimRight(base, "/")
	full := rel
	if strings.HasPrefix(rel, "/") {
		full = base + rel
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := mediaHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		return nil, "", fmt.Errorf("the gateway is still preparing this media; try again in a moment")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("media fetch failed: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, mediaMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(b) > mediaMaxBytes {
		return nil, "", fmt.Errorf("media larger than %d MB", mediaMaxBytes>>20)
	}
	ct := resp.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return b, strings.TrimSpace(ct), nil
}
