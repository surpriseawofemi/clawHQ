// Command probe connects with the stored operator identity and prints one RPC, for
// checking gateway method shapes. Quit ClawHQ first: the two share the device token.
// Usage: go run ./internal/probe <method> [paramsJSON]
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/gateway"
	"github.com/surpriseawofemi/clawhq/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe <method> [paramsJSON]")
		os.Exit(2)
	}
	home, _ := os.UserHomeDir()
	st, err := store.New()
	if err != nil {
		panic(err)
	}
	cfg := st.Read()
	var profile store.GatewayProfile
	for _, g := range cfg.Gateways {
		if g.ID == cfg.ActiveGatewayID {
			profile = g
		}
	}
	conn, err := gateway.New(filepath.Join(home, "Library", "Application Support", "ClawHQ", "identity"), func(gateway.Event) {}, func(gateway.Status) {})
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if _, err := conn.Connect(ctx, profile.ID, profile.URL, gateway.Credential{}); err != nil {
		panic(err)
	}
	var params any = map[string]any{}
	if len(os.Args) > 2 {
		if err := json.Unmarshal([]byte(os.Args[2]), &params); err != nil {
			panic(err)
		}
	}
	raw, err := conn.Request(ctx, os.Args[1], params)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Stdout.Write(raw)
	fmt.Println()
	conn.Disconnect()
}
