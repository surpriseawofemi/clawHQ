// sshprobe runs a few commands on a saved server to see what a Windows box answers.
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/surpriseawofemi/clawhq/internal/sshx"
	"github.com/surpriseawofemi/clawhq/internal/store"
)

func main() {
	st, _ := store.New()
	want := os.Args[1]
	var p store.ServerProfile
	for _, s := range st.Read().Servers {
		if strings.Contains(strings.ToLower(s.Name), strings.ToLower(want)) || s.ID == want {
			p = s
		}
	}
	if p.ID == "" {
		fmt.Println("no such server")
		return
	}
	fmt.Printf("server %s %s@%s platform=%q tmuxOff=%v\n", p.Name, p.User, p.Host, p.Platform, p.TmuxOff)
	home, _ := os.UserHomeDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := sshx.Dial(ctx, sshx.Target{Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, Password: p.Password}, home+"/Library/Application Support/ClawHQ/known_hosts")
	if err != nil {
		fmt.Println("dial:", err)
		return
	}
	defer c.Close()
	run := func(label, cmd string) {
		res, err := sshx.RunRaw(ctx, c, cmd, 30*time.Second)
		out := strings.TrimSpace(res.Stdout + res.Stderr)
		if len(out) > 600 {
			out = out[:600] + "…"
		}
		fmt.Printf("== %s (exit %d, err %v)\n%s\n", label, res.ExitCode, err, out)
	}
	run("shell", "uname -s 2>/dev/null || ver")
	ps := func(script string) string {
		u := []byte{}
		for _, r := range script {
			u = append(u, byte(r), byte(r>>8))
		}
		return "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " + base64.StdEncoding.EncodeToString(u)
	}
	run("psmux lookup", ps("$env:Path = [Environment]::GetEnvironmentVariable('Path','User') + ';' + $env:Path; (Get-Command psmux -ErrorAction SilentlyContinue).Source; psmux -V; psmux ls"))
	run("where", ps("Get-ChildItem -Path $env:LOCALAPPDATA -Filter psmux.exe -Recurse -ErrorAction SilentlyContinue | Select-Object -First 3 -ExpandProperty FullName; [Environment]::GetEnvironmentVariable('Path','User')"))
	// the interactive attach, on a PTY, for a few seconds
	name := "clawhq-probe"
	script := "$env:Path = [Environment]::GetEnvironmentVariable('Path','User') + ';' + $env:Path; psmux has-session -t '" + name + "' 2>$null; if ($LASTEXITCODE -ne 0) { psmux new-session -d -s '" + name + "' }; psmux set -g mouse on 2>$null; psmux set -g status off 2>$null; psmux attach-session -t '" + name + "'"
	cmd := strings.Replace(ps(script), " -NonInteractive", "", 1)
	fmt.Println("== attach:", cmd)
	var buf strings.Builder
	done := make(chan error, 1)
	sh, err := sshx.StartShellCmd(c, 100, 30, cmd, func(b64 string) {
		b, _ := base64.StdEncoding.DecodeString(b64)
		buf.Write(b)
	}, func(e error) { done <- e })
	if err != nil {
		fmt.Println("start:", err)
		return
	}
	select {
	case e := <-done:
		fmt.Println("exited early:", e)
	case <-time.After(8 * time.Second):
		fmt.Println("still running after 8s (good)")
		_ = sh.Write(base64.StdEncoding.EncodeToString([]byte("echo hello-from-psmux\r")))
		time.Sleep(3 * time.Second)
		sh.Close()
	}
	out := buf.String()
	if len(out) > 1500 {
		out = out[len(out)-1500:]
	}
	fmt.Printf("== pty output:\n%q\n", out)
	run("cleanup", ps("$env:Path = [Environment]::GetEnvironmentVariable('Path','User') + ';' + $env:Path; psmux kill-session -t clawhq-probe 2>$null; psmux ls"))
}
