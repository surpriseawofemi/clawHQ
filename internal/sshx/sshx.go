// Package sshx is ClawHQ's SSH client: dial a server with the user's agent, a
// key file or a password, verify the host key against ClawHQ's own known_hosts
// (trust on first use), run commands, and open interactive shells.
package sshx

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Target is everything needed to reach one server.
type Target struct {
	Host     string
	Port     int
	User     string
	Auth     string // "agent", "key" or "password"
	KeyPath  string
	Password string // also the key passphrase for "key"
}

// ErrHostKeyChanged is returned when a known server presents a different key.
var ErrHostKeyChanged = errors.New("the server's host key changed since it was first added; if that is expected, forget the server and add it again")

// Dial opens an SSH connection, learning the host key on first contact.
func Dial(ctx context.Context, t Target, knownHosts string) (*ssh.Client, error) {
	if t.Port == 0 {
		t.Port = 22
	}
	if strings.TrimSpace(t.User) == "" {
		return nil, fmt.Errorf("a user name is required")
	}
	auths, closers, err := authMethods(t)
	for _, c := range closers {
		defer c()
	}
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            t.User,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback(knownHosts),
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(t.Host, fmt.Sprint(t.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	client := ssh.NewClient(c, chans, reqs)
	go keepAlive(client)
	return client, nil
}

// keepAlive sends a small request every 15 seconds so NAT tables between us and
// the server (satellite links, carrier-grade NAT) keep the connection, and a
// quiet terminal is not mistaken for a dead one. Three misses in a row close
// the connection so the terminal can reattach instead of hanging.
func keepAlive(c *ssh.Client) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	misses := 0
	for range t.C {
		done := make(chan error, 1)
		go func() {
			_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				return // the connection is gone; readers see it too
			}
			misses = 0
		case <-time.After(10 * time.Second):
			misses++
			if misses >= 3 {
				_ = c.Close()
				return
			}
		}
	}
}

func authMethods(t Target) ([]ssh.AuthMethod, []func(), error) {
	var out []ssh.AuthMethod
	var closers []func()
	switch t.Auth {
	case "password":
		if t.Password == "" {
			return nil, nil, fmt.Errorf("a password is required")
		}
		out = append(out, ssh.Password(t.Password), ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = t.Password
			}
			return answers, nil
		}))
	case "key":
		path := expandHome(t.KeyPath)
		if path == "" {
			return nil, nil, fmt.Errorf("a key file is required")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read key: %w", err)
		}
		var signer ssh.Signer
		if t.Password != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(t.Password))
		} else {
			signer, err = ssh.ParsePrivateKey(raw)
		}
		if err != nil {
			var pe *ssh.PassphraseMissingError
			if errors.As(err, &pe) {
				return nil, nil, fmt.Errorf("the key is encrypted; enter its passphrase in the password field")
			}
			return nil, nil, fmt.Errorf("parse key: %w", err)
		}
		out = append(out, ssh.PublicKeys(signer))
	default: // agent, with the usual key files as a fallback
		if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
			if conn, err := net.Dial("unix", sock); err == nil {
				ag := agent.NewClient(conn)
				out = append(out, ssh.PublicKeysCallback(ag.Signers))
				closers = append(closers, func() { _ = conn.Close() })
			}
		}
		home, _ := os.UserHomeDir()
		var signers []ssh.Signer
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			raw, err := os.ReadFile(filepath.Join(home, ".ssh", name))
			if err != nil {
				continue
			}
			if s, err := ssh.ParsePrivateKey(raw); err == nil {
				signers = append(signers, s)
			}
		}
		if len(signers) > 0 {
			out = append(out, ssh.PublicKeys(signers...))
		}
		if len(out) == 0 {
			return nil, closers, fmt.Errorf("no SSH agent and no unencrypted key in ~/.ssh; choose a key file or a password")
		}
	}
	return out, closers, nil
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// hostKeyCallback trusts a host on first use and refuses a changed key after that.
func hostKeyCallback(path string) ssh.HostKeyCallback {
	var mu sync.Mutex
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		mu.Lock()
		defer mu.Unlock()
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		if _, err := os.Stat(path); err != nil {
			_ = os.WriteFile(path, nil, 0o600)
		}
		check, err := knownhosts.New(path)
		if err != nil {
			return err
		}
		err = check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			if len(ke.Want) > 0 {
				return ErrHostKeyChanged
			}
			line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
			f, ferr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if ferr != nil {
				return ferr
			}
			defer f.Close()
			_, ferr = f.WriteString(line + "\n")
			return ferr
		}
		return err
	}
}

// Result of one command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Run executes a command through the user's login shell so PATH is what the
// user sees at a prompt (nvm, /opt tools, and so on).
func Run(ctx context.Context, c *ssh.Client, command string, timeout time.Duration) (Result, error) {
	return RunRaw(ctx, c, "bash -lc "+shellQuote(command), timeout)
}

// RunRaw executes a command exactly as given, for servers whose shell is not
// bash (Windows OpenSSH hands it to cmd.exe).
func RunRaw(ctx context.Context, c *ssh.Client, command string, timeout time.Duration) (Result, error) {
	return RunInput(ctx, c, command, "", timeout)
}

// RunInput is RunRaw with text on the command's stdin. It is how long scripts
// reach Windows: cmd.exe caps a command line at 8191 characters, and a script
// base64-encoded as UTF-16 grows almost three times, so anything sizeable is
// piped into "powershell -Command -" instead.
func RunInput(ctx context.Context, c *ssh.Client, command, input string, timeout time.Duration) (Result, error) {
	sess, err := c.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer sess.Close()
	if input != "" {
		sess.Stdin = strings.NewReader(input)
	}
	var out, errb bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errb
	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return Result{Stdout: out.String(), Stderr: errb.String(), ExitCode: -1}, fmt.Errorf("command timed out after %s", timeout)
	}
	res := Result{Stdout: out.String(), Stderr: errb.String()}
	if err != nil {
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitStatus()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Shell is an interactive PTY session.
type Shell struct {
	sess  *ssh.Session
	stdin io.WriteCloser
	once  sync.Once
}

// StartShell opens a login shell on a PTY; every chunk of output goes to onData
// (base64 so binary-safe across JSON) and onExit fires once when it ends.
func StartShell(c *ssh.Client, cols, rows int, onData func(string), onExit func(error)) (*Shell, error) {
	return StartShellCmd(c, cols, rows, "", onData, onExit)
}

// StartShellCmd is StartShell running a command on the PTY instead of the login
// shell (empty command: the login shell). Used to land in tmux.
func StartShellCmd(c *ssh.Client, cols, rows int, command string, onData func(string), onExit func(error)) (*Shell, error) {
	sess, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = sess.Close()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	sess.Stderr = sess.Stdout
	if command == "" {
		err = sess.Shell()
	} else {
		err = sess.Start(command)
	}
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	sh := &Shell{sess: sess, stdin: stdin}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				onData(base64.StdEncoding.EncodeToString(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		werr := sess.Wait()
		sh.once.Do(func() { onExit(werr) })
	}()
	return sh, nil
}

// Write sends keystrokes (base64 of the raw bytes).
func (s *Shell) Write(b64 string) error {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	_, err = s.stdin.Write(b)
	return err
}

// Resize tells the remote PTY the new size.
func (s *Shell) Resize(cols, rows int) error {
	return s.sess.WindowChange(rows, cols)
}

// Close ends the session.
func (s *Shell) Close() {
	_ = s.stdin.Close()
	_ = s.sess.Close()
}

// Command is a running non-interactive command whose output streams back.
type Command struct {
	sess *ssh.Session
}

// StartCommand runs a command through the login shell without a PTY; stdout and
// stderr arrive interleaved through onData (base64), onExit carries the exit code
// (-1 when the session died) once.
func StartCommand(c *ssh.Client, command string, onData func(string), onExit func(code int, err error)) (*Command, error) {
	return StartCommandRaw(c, "bash -lc "+shellQuote(command), onData, onExit)
}

// StartCommandRaw is StartCommand without the bash wrapper.
func StartCommandRaw(c *ssh.Client, command string, onData func(string), onExit func(code int, err error)) (*Command, error) {
	sess, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	if err := sess.Start(command); err != nil {
		_ = sess.Close()
		return nil, err
	}
	cmd := &Command{sess: sess}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				onData(base64.StdEncoding.EncodeToString(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		werr := sess.Wait()
		_ = pw.Close()
		code := 0
		if werr != nil {
			code = -1
			var ee *ssh.ExitError
			if errors.As(werr, &ee) {
				code = ee.ExitStatus()
				werr = nil
			}
		}
		onExit(code, werr)
		_ = sess.Close()
	}()
	return cmd, nil
}

// Stop interrupts the command.
func (c *Command) Stop() {
	_ = c.sess.Signal(ssh.SIGINT)
	time.AfterFunc(2*time.Second, func() { _ = c.sess.Close() })
}
