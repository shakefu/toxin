//go:build unix

package toxin

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// 20-api@Config.Get: a child write completed after the parent file read
// starts and before it finishes wins over the inherited value. The FIFO
// enforces that ordering without scheduling guesses.
func TestNoEtcdInheritedResultPreservesConcurrentChildSet(t *testing.T) {
	clearToxinEnv(t)
	dir := t.TempDir()
	fc := NewFileCache("parent/", dir)
	if err := syscall.Mkfifo(fc.Filename(), 0o600); err != nil {
		t.Fatalf("creating parent cache FIFO: %v", err)
	}
	child, err := New(WithPrefix("child/"), WithFileCache(dir), WithInheritPrefix("parent/"))
	if err != nil {
		t.Fatalf("New child: %v", err)
	}

	getDone := make(chan error, 1)
	go func() {
		_, err := child.Get("app.mode", "fallback")
		getDone <- err
	}()

	// Opening a FIFO writer blocks until the inherited reader has opened it.
	writerReady := make(chan *os.File, 1)
	writerErr := make(chan error, 1)
	go func() {
		writer, err := os.OpenFile(fc.Filename(), os.O_WRONLY, 0)
		if err != nil {
			writerErr <- err
			return
		}
		writerReady <- writer
	}()
	var writer *os.File
	select {
	case writer = <-writerReady:
	case err := <-writerErr:
		t.Fatalf("opening parent FIFO writer: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("inherited Get did not start reading the parent file")
	}

	defer func() { _ = writer.Close() }()

	// The reader is now blocked waiting for JSON. Complete the child write
	// before allowing the inherited result to reach the child cache. Both the
	// timeout and deferred close unblock the reader if Set regresses.
	setDone := make(chan error, 1)
	go func() { setDone <- child.Set("app.mode", "custom") }()
	select {
	case err := <-setDone:
		if err != nil {
			t.Fatalf("Set child value: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child Set did not finish")
	}
	if _, err := writer.Write([]byte(`{"app.mode":"standard"}`)); err != nil {
		t.Fatalf("writing parent JSON: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing parent FIFO writer: %v", err)
	}

	select {
	case err := <-getDone:
		if err != nil {
			t.Fatalf("inherited Get: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("inherited Get did not finish")
	}
	got, err := child.GetStrict("app.mode")
	if err != nil || got != "custom" {
		t.Errorf("inherited read replaced child write with (%v, %v), want custom", got, err)
	}
}
