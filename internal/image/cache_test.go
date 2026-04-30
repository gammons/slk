package image

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCache_PutGet(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCache(dir, 10) // 10 MB cap
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("hello-png-bytes")
	path, err := c.Put("k1", "png", data)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := c.Get("k1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got != path {
		t.Errorf("Get path %q != Put path %q", got, path)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("path does not exist: %v", err)
	}
}

func TestCache_Miss(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 10)
	if _, ok := c.Get("missing"); ok {
		t.Fatal("expected miss")
	}
}

func TestCache_LRUEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	// 1 MB cap; entries ~ 600KB each => 2nd Put fits, 3rd evicts oldest.
	c, _ := NewCache(dir, 1)
	bigA := bytes.Repeat([]byte{'a'}, 600*1024)
	bigB := bytes.Repeat([]byte{'b'}, 600*1024)
	bigC := bytes.Repeat([]byte{'c'}, 600*1024)

	if _, err := c.Put("a", "bin", bigA); err != nil {
		t.Fatal(err)
	}
	// Make 'a' older than 'b' by tweaking mtime.
	older := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "a.bin"), older, older)

	if _, err := c.Put("b", "bin", bigB); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Put("c", "bin", bigC); err != nil {
		t.Fatal(err)
	}

	if _, ok := c.Get("a"); ok {
		t.Errorf("expected 'a' evicted")
	}
	if _, ok := c.Get("b"); !ok {
		t.Errorf("expected 'b' still present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Errorf("expected 'c' present")
	}
}

func TestCache_OversizeEntryAllowed(t *testing.T) {
	c, _ := NewCache(t.TempDir(), 1) // 1 MB cap
	huge := bytes.Repeat([]byte{'x'}, 2*1024*1024)
	if _, err := c.Put("huge", "bin", huge); err != nil {
		t.Fatalf("oversize Put should succeed: %v", err)
	}
	if _, ok := c.Get("huge"); !ok {
		t.Error("expected oversize entry served from cache for this session")
	}
}

func TestCache_GetUpdatesATime(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCache(dir, 10)
	c.Put("k", "bin", []byte("x"))

	older := time.Now().Add(-time.Hour)
	path := filepath.Join(dir, "k.bin")
	os.Chtimes(path, older, older)

	c.Get("k")

	st, _ := os.Stat(path)
	if time.Since(st.ModTime()) > time.Minute {
		t.Errorf("Get should refresh mtime, got %v old", time.Since(st.ModTime()))
	}
}

func TestCache_LoadIndexAtStartup(t *testing.T) {
	dir := t.TempDir()
	c1, _ := NewCache(dir, 10)
	c1.Put("preexisting", "bin", []byte("data"))

	// New cache instance: should pick up the existing file.
	c2, err := NewCache(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.Get("preexisting"); !ok {
		t.Error("expected pre-existing entry to be indexed at startup")
	}
}
