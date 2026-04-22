// Package logstore appends container log lines to rotated, gzip-compressed
// files on disk. One active file per (agent, container); rotations and
// historical reads live beside it.
package logstore

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Line struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Stream        string    `json:"stream"`
	Timestamp     time.Time `json:"timestamp"`
	Text          string    `json:"text"`
}

type Config struct {
	Dir            string
	MaxFileBytes   int64
	MaxRotations   int
	RetentionDays  int
}

type Store struct {
	cfg Config

	mu      sync.Mutex
	writers map[string]*writer // keyed by agentID/containerName
}

type writer struct {
	path string
	f    *os.File
	size int64
	bw   *bufio.Writer
}

func New(cfg Config) (*Store, error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir logs: %w", err)
	}
	return &Store{cfg: cfg, writers: map[string]*writer{}}, nil
}

// Append writes one line to the active file for its (agent, container).
// Rotates when MaxFileBytes is exceeded.
func (s *Store) Append(agentID string, l Line) error {
	key := agentID + "/" + safeName(l.ContainerName)
	s.mu.Lock()
	defer s.mu.Unlock()

	w, err := s.writerFor(agentID, l.ContainerName, key)
	if err != nil {
		return err
	}

	// Stored format: one JSON object per line (ndjson). Small + greppable.
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	n, err := w.bw.Write(data)
	if err != nil {
		return err
	}
	w.size += int64(n)

	if w.size >= s.cfg.MaxFileBytes {
		return s.rotateLocked(key, w)
	}
	return nil
}

// Flush forces a flush of all buffered writers to disk.
func (s *Store) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.writers {
		w.bw.Flush()
		w.f.Sync()
	}
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.writers {
		w.bw.Flush()
		w.f.Close()
	}
	s.writers = map[string]*writer{}
}

func (s *Store) writerFor(agentID, containerName, key string) (*writer, error) {
	if w, ok := s.writers[key]; ok {
		return w, nil
	}
	dir := filepath.Join(s.cfg.Dir, agentID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, safeName(containerName)+".log")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	w := &writer{path: path, f: f, size: info.Size(), bw: bufio.NewWriter(f)}
	s.writers[key] = w
	return w, nil
}

// rotateLocked closes the active file, gzips it with a timestamped suffix,
// drops old rotations beyond MaxRotations, and opens a fresh active file.
func (s *Store) rotateLocked(key string, w *writer) error {
	w.bw.Flush()
	w.f.Close()
	delete(s.writers, key)

	stamp := time.Now().UTC().Format("20060102T150405")
	gzPath := w.path + "." + stamp + ".gz"
	if err := gzipFile(w.path, gzPath); err != nil {
		return err
	}
	if err := os.Remove(w.path); err != nil {
		return err
	}
	s.pruneRotations(w.path)
	return nil
}

// pruneRotations removes the oldest .gz rotations when more than MaxRotations
// exist for this container.
func (s *Store) pruneRotations(activePath string) {
	dir := filepath.Dir(activePath)
	base := filepath.Base(activePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var gz []string
	prefix := base + "."
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".gz") {
			gz = append(gz, filepath.Join(dir, n))
		}
	}
	if len(gz) <= s.cfg.MaxRotations {
		return
	}
	sort.Strings(gz) // timestamp-in-name → lexicographic == chronological
	for _, p := range gz[:len(gz)-s.cfg.MaxRotations] {
		os.Remove(p)
	}
}

// PruneOld deletes any .gz rotations older than RetentionDays. Called on a
// periodic ticker by the server.
func (s *Store) PruneOld() {
	if s.cfg.RetentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(s.cfg.RetentionDays) * 24 * time.Hour)
	_ = filepath.Walk(s.cfg.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".gz") {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(path)
		}
		return nil
	})
}

// Tail returns the last n lines from the active file for (agentID, name).
// Historical reads from rotated .gz files are not yet implemented — the
// rotation cap (MaxFileBytes × MaxRotations) bounds recoverable history.
func (s *Store) Tail(agentID, name string, n int) ([]Line, error) {
	path := filepath.Join(s.cfg.Dir, agentID, safeName(name)+".log")

	s.mu.Lock()
	if w, ok := s.writers[agentID+"/"+safeName(name)]; ok {
		w.bw.Flush()
	}
	s.mu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	lines := tailLines(f, n)
	out := make([]Line, 0, len(lines))
	for _, raw := range lines {
		var l Line
		if err := json.Unmarshal(raw, &l); err == nil {
			out = append(out, l)
		}
	}
	return out, nil
}

// tailLines reads up to n trailing lines from f by scanning backwards in 32KB
// chunks. Good enough for log tail queries of a few thousand lines.
func tailLines(f *os.File, n int) [][]byte {
	const chunk = 32 * 1024
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	if size == 0 || n <= 0 {
		return nil
	}

	var (
		buf      bytes.Buffer
		off      = size
		newlines int
	)
	tmp := make([]byte, chunk)
	for off > 0 && newlines <= n {
		sz := int64(chunk)
		if off < sz {
			sz = off
		}
		off -= sz
		if _, err := f.ReadAt(tmp[:sz], off); err != nil && err != io.EOF {
			return nil
		}
		// Prepend chunk (buffer is building backwards).
		grown := bytes.NewBuffer(make([]byte, 0, int(sz)+buf.Len()))
		grown.Write(tmp[:sz])
		grown.Write(buf.Bytes())
		buf = *grown
		newlines = bytes.Count(buf.Bytes(), []byte("\n"))
	}

	all := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(all) > n {
		all = all[len(all)-n:]
	}
	// Copy out — underlying buf will be GC'd.
	out := make([][]byte, len(all))
	for i, b := range all {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		return err
	}
	return gz.Close()
}

// safeName strips characters that would escape the log directory and keeps
// filenames stable across OSes.
func safeName(name string) string {
	r := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		"..", "_",
		":", "_",
	)
	s := r.Replace(name)
	if s == "" {
		s = "unnamed"
	}
	return s
}
