package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/node"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// AgentNotification is what the UI gets for a banner: the agent's request plus the
// display name looked up on the gateway.
type AgentNotification struct {
	node.Notification
	AgentName  string `json:"agentName,omitempty"`
	AgentEmoji string `json:"agentEmoji,omitempty"`
}

// notifier turns a node-role system.notify into an OS notification from ClawHQ and
// an in-app banner, both naming the agent that asked.
//
// The gateway's own notify action carries no sender, so when the request has no
// agent id the notifier looks at which agent is mid-run on the gateway at that
// moment; one agent running means it was that one. ClawHQ's ask-a-human tool sends
// the id outright, so agents that use it are always named.
type notifier struct {
	app     *application.App
	conn    *gateway.Conn
	native  *notifications.NotificationService
	once    sync.Once
	granted bool
	seq     int
}

func newNotifier(conn *gateway.Conn, native *notifications.NotificationService) *notifier {
	return &notifier{conn: conn, native: native}
}

// authorize asks macOS once for permission to post notifications. Other platforms
// return true straight away.
func (n *notifier) authorize() {
	n.once.Do(func() {
		if n.native == nil {
			return
		}
		ok, err := n.native.RequestNotificationAuthorization()
		if err != nil {
			log.Printf("notifications: %v", err)
		}
		n.granted = ok
	})
}

// deliver is the node's onNotify hook.
func (n *notifier) deliver(raw node.Notification) {
	go func() {
		out := AgentNotification{Notification: raw}
		if out.AgentID == "" {
			out.AgentID = n.guessAgent()
		}
		if out.AgentID != "" {
			out.AgentName, out.AgentEmoji = n.agentIdentity(out.AgentID)
		}
		if n.app != nil {
			n.app.Event.Emit("node:notify", out)
		}
		n.authorize()
		if n.native == nil || !n.granted {
			return
		}
		n.seq++
		who := out.AgentName
		if who == "" {
			who = out.AgentID
		}
		if who == "" {
			who = "An agent"
		}
		opts := notifications.NotificationOptions{
			ID:       fmt.Sprintf("clawhq-notify-%d-%d", time.Now().UnixNano(), n.seq),
			Title:    out.Title,
			Subtitle: who + " is asking",
			Body:     out.Body,
			Sound:    &notifications.NotificationSound{Name: "default"},
			ThreadID: "clawhq-agents",
			Data:     map[string]interface{}{"agentId": out.AgentID, "sessionKey": out.SessionKey},
		}
		if err := n.native.SendNotification(opts); err != nil {
			log.Printf("notifications: %v", err)
		}
	}()
}

// guessAgent returns the id of the one agent with a run in flight, or "" when the
// answer is not unambiguous.
func (n *notifier) guessAgent() string {
	if n.conn.Status().Phase != gateway.PhaseConnected {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := n.conn.Request(ctx, "sessions.list", nil)
	if err != nil {
		return ""
	}
	var res struct {
		Items    []sessionRow `json:"items"`
		Sessions []sessionRow `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return ""
	}
	rows := res.Items
	if len(rows) == 0 {
		rows = res.Sessions
	}
	running := map[string]bool{}
	for _, s := range rows {
		if s.HasActiveRun && s.AgentID != "" {
			running[s.AgentID] = true
		}
	}
	if len(running) != 1 {
		return ""
	}
	for id := range running {
		return id
	}
	return ""
}

type sessionRow struct {
	AgentID      string `json:"agentId"`
	HasActiveRun bool   `json:"hasActiveRun"`
}

// agentIdentity looks up an agent's display name and emoji.
func (n *notifier) agentIdentity(agentID string) (string, string) {
	if n.conn.Status().Phase != gateway.PhaseConnected {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	raw, err := n.conn.Request(ctx, "agents.list", nil)
	if err != nil {
		return "", ""
	}
	var res struct {
		Agents []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Identity struct {
				Name  string `json:"name"`
				Emoji string `json:"emoji"`
			} `json:"identity"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", ""
	}
	for _, a := range res.Agents {
		if a.ID == agentID {
			name := strings.TrimSpace(a.Identity.Name)
			if name == "" {
				name = strings.TrimSpace(a.Name)
			}
			return name, strings.TrimSpace(a.Identity.Emoji)
		}
	}
	return "", ""
}
