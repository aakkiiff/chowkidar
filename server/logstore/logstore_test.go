package logstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T, maxFileBytes int64) *Store {
	t.Helper()
	s, err := New(Config{
		Dir:           t.TempDir(),
		MaxFileBytes:  maxFileBytes,
		MaxRotations:  3,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func makeLine(name, text string) Line {
	return Line{
		ContainerID:   "cid123",
		ContainerName: name,
		Stream:        "stdout",
		Timestamp:     time.Now(),
		Text:          text,
	}
}

func TestAppend_Tail_RoundTrip(t *testing.T) {
	s := newTestStore(t, 1024*1024)
	const n = 10
	for i := 0; i < n; i++ {
		if err := s.Append("agent1", makeLine("webapp", fmt.Sprintf("line %d", i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	s.Flush()
	lines, err := s.Tail("agent1", "webapp", n)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(lines) != n {
		t.Errorf("expected %d lines, got %d", n, len(lines))
	}
}

func TestTail_NonExistentContainer_ReturnsNil(t *testing.T) {
	s := newTestStore(t, 1024*1024)
	lines, err := s.Tail("agent1", "nonexistent", 100)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if lines != nil {
		t.Errorf("expected nil slice, got %v", lines)
	}
}

func TestRotation_ActiveFileSmallerAfterRotation(t *testing.T) {
	// MaxFileBytes = 512 — small enough that a few lines trigger rotation.
	s := newTestStore(t, 512)
	bigText := strings.Repeat("x", 100)
	for i := 0; i < 20; i++ {
		if err := s.Append("agrot", makeLine("svc", bigText)); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	s.Flush()

	dir := filepath.Join(s.cfg.Dir, "agrot")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var gzFound bool
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".gz") {
			gzFound = true
			break
		}
	}
	if !gzFound {
		t.Error("expected at least one .gz rotation file")
	}
	// The active file (if it exists) should be smaller than total bytes written.
	activePath := filepath.Join(dir, "svc.log")
	info, err := os.Stat(activePath)
	if err == nil && info.Size() >= int64(20*100) {
		t.Errorf("active file should be smaller than total bytes, got %d", info.Size())
	}
}

func TestPruneOrphans_RemovesUnknownAgent(t *testing.T) {
	s := newTestStore(t, 1024*1024)
	// Append a line to create the directory.
	if err := s.Append("orphan-agent", makeLine("svc", "hello")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	s.Flush()
	s.Close()

	// PruneOrphans with an empty valid set.
	s.PruneOrphans(map[string]struct{}{})

	dir := filepath.Join(s.cfg.Dir, "orphan-agent")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected orphan-agent dir to be removed")
	}
}

func TestDeleteAgent_RemovesLogDir(t *testing.T) {
	s := newTestStore(t, 1024*1024)
	if err := s.Append("delagent", makeLine("svc", "hello")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	s.Flush()
	if err := s.DeleteAgent("delagent"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	dir := filepath.Join(s.cfg.Dir, "delagent")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected delagent dir to be removed")
	}
}

func TestSafeName_StripsSeparators(t *testing.T) {
	s := newTestStore(t, 1024*1024)
	// Using "/" in container name should not escape the log directory.
	containerName := "../../etc/passwd"
	if err := s.Append("safeagent", makeLine(containerName, "hello")); err != nil {
		t.Fatalf("Append with path traversal name: %v", err)
	}
	s.Flush()
	// The file should be inside the agent dir (no path traversal).
	expectedPath := filepath.Join(s.cfg.Dir, "safeagent", safeName(containerName)+".log")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Errorf("expected log file at safe path %s, got error: %v", expectedPath, err)
	}
}
