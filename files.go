package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/surpriseawofemi/clawhq/internal/sshx"
	"github.com/surpriseawofemi/clawhq/internal/store"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/crypto/ssh"
)

// FileService is the SFTP side of Servers: browse, read, edit, upload and
// download files on a server. One SFTP connection per server is kept open and
// dropped after a few idle minutes.
type FileService struct {
	store *store.Store
	app   *application.App
	mu    sync.Mutex
	pool  map[string]*sftpConn
}

type sftpConn struct {
	client   *ssh.Client
	sftp     *sftp.Client
	lastUsed time.Time
}

type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	ModMs int64  `json:"modMs"`
	Mode  string `json:"mode"`
	Link  bool   `json:"link"`
}

type DirListing struct {
	Path    string      `json:"path"`
	Parent  string      `json:"parent"`
	Home    string      `json:"home"`
	Entries []FileEntry `json:"entries"`
}

type FileContent struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ModMs     int64  `json:"modMs"`
	Mime      string `json:"mime"`
	IsText    bool   `json:"isText"`
	Text      string `json:"text,omitempty"`
	Truncated bool   `json:"truncated"`
	DataURL   string `json:"dataUrl,omitempty"`
}

const (
	editMaxBytes    = 2 << 20  // editor refuses larger text files
	previewMaxBytes = 12 << 20 // inline image preview
	sftpIdle        = 5 * time.Minute
)

func (s *FileService) conn(ctx context.Context, id string) (*sftpConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.pool[id]; ok {
		// A dead connection shows up as a failed stat.
		if _, err := c.sftp.Getwd(); err == nil {
			c.lastUsed = time.Now()
			return c, nil
		}
		c.sftp.Close()
		c.client.Close()
		delete(s.pool, id)
	}
	var p store.ServerProfile
	found := false
	for _, cur := range s.store.Read().Servers {
		if cur.ID == id {
			p, found = cur, true
		}
	}
	if !found {
		return nil, fmt.Errorf("unknown server %q", id)
	}
	client, err := sshx.Dial(ctx, sshx.Target{Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, KeyPath: p.KeyPath, Password: p.Password}, knownHostsPath())
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("sftp: %w (is the SFTP subsystem enabled in sshd?)", err)
	}
	if s.pool == nil {
		s.pool = map[string]*sftpConn{}
		go s.reap()
	}
	c := &sftpConn{client: client, sftp: sc, lastUsed: time.Now()}
	s.pool[id] = c
	return c, nil
}

// drop closes the pooled SFTP connection to one server.
func (s *FileService) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.pool[id]; ok {
		c.sftp.Close()
		c.client.Close()
		delete(s.pool, id)
	}
}

func (s *FileService) reap() {
	for range time.Tick(time.Minute) {
		s.mu.Lock()
		for id, c := range s.pool {
			if time.Since(c.lastUsed) > sftpIdle {
				c.sftp.Close()
				c.client.Close()
				delete(s.pool, id)
			}
		}
		s.mu.Unlock()
	}
}

func cleanRemote(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// List returns a directory; an empty path means the user's home.
func (s *FileService) List(ctx context.Context, id, dir string) (DirListing, error) {
	c, err := s.conn(ctx, id)
	if err != nil {
		return DirListing{}, err
	}
	home, _ := c.sftp.Getwd()
	dir = cleanRemote(dir)
	if dir == "" {
		dir = home
	}
	if strings.HasPrefix(dir, "~") {
		dir = path.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), "/"))
	}
	infos, err := c.sftp.ReadDir(dir)
	if err != nil {
		return DirListing{}, err
	}
	out := DirListing{Path: dir, Parent: path.Dir(dir), Home: home}
	for _, fi := range infos {
		e := FileEntry{Name: fi.Name(), Path: path.Join(dir, fi.Name()), IsDir: fi.IsDir(), Size: fi.Size(), ModMs: fi.ModTime().UnixMilli(), Mode: fi.Mode().String(), Link: fi.Mode()&os.ModeSymlink != 0}
		if e.Link {
			if st, err := c.sftp.Stat(e.Path); err == nil {
				e.IsDir = st.IsDir()
				e.Size = st.Size()
			}
		}
		out.Entries = append(out.Entries, e)
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		if out.Entries[i].IsDir != out.Entries[j].IsDir {
			return out.Entries[i].IsDir
		}
		return strings.ToLower(out.Entries[i].Name) < strings.ToLower(out.Entries[j].Name)
	})
	if out.Entries == nil {
		out.Entries = []FileEntry{}
	}
	return out, nil
}

// Read opens a file for the editor (text) or a preview (images).
func (s *FileService) Read(ctx context.Context, id, p string) (FileContent, error) {
	c, err := s.conn(ctx, id)
	if err != nil {
		return FileContent{}, err
	}
	p = cleanRemote(p)
	st, err := c.sftp.Stat(p)
	if err != nil {
		return FileContent{}, err
	}
	if st.IsDir() {
		return FileContent{}, fmt.Errorf("%s is a folder", p)
	}
	out := FileContent{Path: p, Size: st.Size(), ModMs: st.ModTime().UnixMilli()}
	out.Mime = mime.TypeByExtension(strings.ToLower(path.Ext(p)))
	if i := strings.IndexByte(out.Mime, ';'); i >= 0 {
		out.Mime = out.Mime[:i]
	}
	f, err := c.sftp.Open(p)
	if err != nil {
		return FileContent{}, err
	}
	defer f.Close()
	if strings.HasPrefix(out.Mime, "image/") {
		if st.Size() > previewMaxBytes {
			return out, nil
		}
		b, err := io.ReadAll(f)
		if err != nil {
			return FileContent{}, err
		}
		out.DataURL = "data:" + out.Mime + ";base64," + base64.StdEncoding.EncodeToString(b)
		return out, nil
	}
	limit := int64(editMaxBytes)
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return FileContent{}, err
	}
	if int64(len(b)) > limit {
		b = b[:limit]
		out.Truncated = true
	}
	head := b
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		out.IsText = false
		return out, nil
	}
	out.IsText = true
	if out.Mime == "" {
		out.Mime = "text/plain"
	}
	out.Text = string(b)
	return out, nil
}

// Write saves the editor's text over the file (write to a temp name, then rename).
func (s *FileService) Write(ctx context.Context, id, p, text string) (FileContent, error) {
	c, err := s.conn(ctx, id)
	if err != nil {
		return FileContent{}, err
	}
	p = cleanRemote(p)
	tmp := p + ".clawhq-tmp"
	f, err := c.sftp.Create(tmp)
	if err != nil {
		return FileContent{}, err
	}
	if _, err := f.Write([]byte(text)); err != nil {
		f.Close()
		_ = c.sftp.Remove(tmp)
		return FileContent{}, err
	}
	f.Close()
	if st, err := c.sftp.Stat(p); err == nil {
		_ = c.sftp.Chmod(tmp, st.Mode().Perm())
	}
	if err := c.sftp.PosixRename(tmp, p); err != nil {
		if err2 := c.sftp.Rename(tmp, p); err2 != nil {
			_ = c.sftp.Remove(tmp)
			return FileContent{}, err
		}
	}
	return s.Read(ctx, id, p)
}

func (s *FileService) Mkdir(ctx context.Context, id, p string) error {
	c, err := s.conn(ctx, id)
	if err != nil {
		return err
	}
	return c.sftp.MkdirAll(cleanRemote(p))
}

// Touch creates an empty file.
func (s *FileService) Touch(ctx context.Context, id, p string) error {
	c, err := s.conn(ctx, id)
	if err != nil {
		return err
	}
	p = cleanRemote(p)
	if _, err := c.sftp.Stat(p); err == nil {
		return fmt.Errorf("%s exists already", p)
	}
	f, err := c.sftp.Create(p)
	if err != nil {
		return err
	}
	return f.Close()
}

func (s *FileService) Rename(ctx context.Context, id, from, to string) error {
	c, err := s.conn(ctx, id)
	if err != nil {
		return err
	}
	from, to = cleanRemote(from), cleanRemote(to)
	if err := c.sftp.PosixRename(from, to); err != nil {
		return c.sftp.Rename(from, to)
	}
	return nil
}

// Delete removes a file, or a folder and everything in it.
func (s *FileService) Delete(ctx context.Context, id, p string) error {
	c, err := s.conn(ctx, id)
	if err != nil {
		return err
	}
	p = cleanRemote(p)
	if p == "/" || p == "" {
		return fmt.Errorf("refusing to delete %q", p)
	}
	st, err := c.sftp.Stat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return c.sftp.Remove(p)
	}
	return c.sftp.RemoveAll(p)
}

type Transfer struct {
	ServerID string `json:"serverId"`
	Name     string `json:"name"`
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Finished bool   `json:"finished"`
	Error    string `json:"error,omitempty"`
}

func (s *FileService) progress(t Transfer) {
	if s.app != nil {
		s.app.Event.Emit("files:progress", t)
	}
}

type progressWriter struct {
	w    io.Writer
	done int64
	tick func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	p.tick(p.done)
	return n, err
}

// Upload asks for local files, then copies them into the remote folder. Progress
// arrives as files:progress events. Returns the names uploaded.
func (s *FileService) Upload(ctx context.Context, id, dir string) ([]string, error) {
	app := application.Get()
	if app == nil {
		return nil, fmt.Errorf("no application")
	}
	paths, err := app.Dialog.OpenFile().SetTitle("Upload to the server").CanChooseFiles(true).CanChooseDirectories(false).PromptForMultipleSelection()
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return s.UploadPaths(ctx, id, dir, paths)
}

// UploadPaths copies the given local files into the remote folder.
func (s *FileService) UploadPaths(ctx context.Context, id, dir string, paths []string) ([]string, error) {
	c, err := s.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	dir = cleanRemote(dir)
	var names []string
	for _, local := range paths {
		name := filepath.Base(local)
		remote := path.Join(dir, name)
		st, err := os.Stat(local)
		if err != nil {
			s.progress(Transfer{ServerID: id, Name: name, Finished: true, Error: err.Error()})
			continue
		}
		if st.IsDir() {
			s.progress(Transfer{ServerID: id, Name: name, Finished: true, Error: "folders are not uploaded yet; zip it first"})
			continue
		}
		src, err := os.Open(local)
		if err != nil {
			s.progress(Transfer{ServerID: id, Name: name, Finished: true, Error: err.Error()})
			continue
		}
		dst, err := c.sftp.Create(remote)
		if err != nil {
			src.Close()
			s.progress(Transfer{ServerID: id, Name: name, Finished: true, Error: err.Error()})
			continue
		}
		total := st.Size()
		last := time.Now()
		pw := &progressWriter{w: dst, tick: func(done int64) {
			if time.Since(last) > 150*time.Millisecond || done == total {
				last = time.Now()
				s.progress(Transfer{ServerID: id, Name: name, Done: done, Total: total})
			}
		}}
		_, err = io.Copy(pw, src)
		src.Close()
		dst.Close()
		if err != nil {
			s.progress(Transfer{ServerID: id, Name: name, Finished: true, Error: err.Error()})
			continue
		}
		s.progress(Transfer{ServerID: id, Name: name, Done: total, Total: total, Finished: true})
		names = append(names, name)
		log.Printf("files: uploaded %s to %s", name, remote)
	}
	return names, nil
}

// Download saves a remote file where the user chooses. Returns the local path.
func (s *FileService) Download(ctx context.Context, id, p string) (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("no application")
	}
	c, err := s.conn(ctx, id)
	if err != nil {
		return "", err
	}
	p = cleanRemote(p)
	local, err := app.Dialog.SaveFile().SetMessage("Save file").SetFilename(path.Base(p)).CanCreateDirectories(true).PromptForSingleSelection()
	if err != nil || local == "" {
		return "", err
	}
	src, err := c.sftp.Open(p)
	if err != nil {
		return "", err
	}
	defer src.Close()
	st, _ := src.Stat()
	dst, err := os.Create(local)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	total := int64(0)
	if st != nil {
		total = st.Size()
	}
	name := path.Base(p)
	last := time.Now()
	pw := &progressWriter{w: dst, tick: func(done int64) {
		if time.Since(last) > 150*time.Millisecond || done == total {
			last = time.Now()
			s.progress(Transfer{ServerID: id, Name: name, Done: done, Total: total})
		}
	}}
	if _, err := io.Copy(pw, src); err != nil {
		s.progress(Transfer{ServerID: id, Name: name, Finished: true, Error: err.Error()})
		return "", err
	}
	s.progress(Transfer{ServerID: id, Name: name, Done: total, Total: total, Finished: true})
	return local, nil
}

func fsMode(m uint32) os.FileMode { return os.FileMode(m) }
