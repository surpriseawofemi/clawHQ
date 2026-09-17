package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/store"
)

// serverMonitor re-checks every watched server on a timer and rings the bell
// when one stops answering, its disk fills up, or its load runs away.
type serverMonitor struct {
	servers *ServerService
	notify  *notifier
	mu      sync.Mutex
	fails   map[string]int
	warned  map[string]time.Time // key: serverID+":"+kind
}

const (
	monitorEvery   = 10 * time.Minute
	monitorRepeat  = 6 * time.Hour // how often the same warning may repeat
	diskWarnUsed   = 90
	loadWarnFactor = 2.0
)

var usedPctRe = regexp.MustCompile(`\((\d+)% used\)`)

func (m *serverMonitor) run() {
	m.fails = map[string]int{}
	m.warned = map[string]time.Time{}
	time.Sleep(90 * time.Second)
	for {
		for _, p := range m.servers.store.Read().Servers {
			if p.MonitorOff {
				continue
			}
			m.check(p)
		}
		time.Sleep(monitorEvery)
	}
}

func (m *serverMonitor) warn(p store.ServerProfile, kind, title, body string) {
	key := p.ID + ":" + kind
	m.mu.Lock()
	last, seen := m.warned[key]
	if seen && time.Since(last) < monitorRepeat {
		m.mu.Unlock()
		return
	}
	m.warned[key] = time.Now()
	m.mu.Unlock()
	if m.notify != nil {
		m.notify.show(store.Notice{Title: title, Body: body, AgentName: p.Name, AgentEmoji: "🖥️", Origin: "server:" + p.ID, AtMs: time.Now().UnixMilli()})
	}
}

func (m *serverMonitor) check(p store.ServerProfile) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	h := m.servers.Health(ctx, p.ID)
	if !h.OK {
		m.mu.Lock()
		m.fails[p.ID]++
		n := m.fails[p.ID]
		m.mu.Unlock()
		// Two misses in a row, so a single hiccup stays quiet.
		if n == 2 {
			m.warn(p, "down", "Server not answering · "+p.Name, fmt.Sprintf("%s@%s has failed two health checks: %s", p.User, p.Host, h.Error))
		}
		return
	}
	m.mu.Lock()
	wasDown := m.fails[p.ID] >= 2
	m.fails[p.ID] = 0
	if wasDown {
		delete(m.warned, p.ID+":down")
	}
	m.mu.Unlock()
	if wasDown {
		m.warn(p, "up", "Server back · "+p.Name, fmt.Sprintf("%s answers again.", p.Host))
	}
	if mm := usedPctRe.FindStringSubmatch(h.Disk); len(mm) == 2 {
		if used, _ := strconv.Atoi(mm[1]); used >= diskWarnUsed {
			m.warn(p, "disk", "Disk almost full · "+p.Name, fmt.Sprintf("/ is %d%% used: %s", used, h.Disk))
		}
	}
	if h.Cores > 0 {
		if f := strings.Fields(h.Load); len(f) > 0 {
			if l1, err := strconv.ParseFloat(strings.TrimSuffix(f[0], ","), 64); err == nil && l1 > float64(h.Cores)*loadWarnFactor {
				m.warn(p, "load", "High load · "+p.Name, fmt.Sprintf("load %s on %d cores", h.Load, h.Cores))
			}
		}
	}
}
