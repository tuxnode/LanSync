package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
)

func TestWatcherBasic(t *testing.T) {
	tempDir := t.TempDir()
	w, err := NewWatcher(tempDir)
	if err != nil {
		t.Fatalf("NewWatcher failed: %v", err)
	}
	defer w.WatcherStop()

	resChan := make(chan protocol.SyncMessage, 1)
	w.OnMessage = func(msg protocol.SyncMessage) {
		resChan <- msg
	}
	w.WatcherStart()
	time.Sleep(50 * time.Millisecond)

	testFile := filepath.Join(tempDir, "test.txt")
	ignoreFile := filepath.Join(tempDir, "ignore.txt")
	w.AddIgnorePath(ignoreFile)

	if err := os.WriteFile(ignoreFile, []byte("I'm from network"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("Test WriteFile"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	select {
	case msg := <-resChan:
		if msg.RelPath != "test.txt" {
			t.Errorf("Path mismatch: %s", msg.RelPath)
		}
		if msg.Type != protocol.MsgNotify {
			t.Errorf("Type mismatch: %v", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write file event timed out")
	}

	// 验证 ignore 文件不会触发回调
	select {
	case msg := <-resChan:
		if msg.RelPath == "ignore.txt" {
			t.Error("Ignored file triggered callback")
		}
	default:
	}

	// 清理后测试递归目录监听
	os.Remove(testFile)
	os.Remove(ignoreFile)
	time.Sleep(100 * time.Millisecond)

	nestedPath := filepath.Join(tempDir, "sub", "test", "deep", "nop")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	recurFile := filepath.Join(nestedPath, "recurFile.txt")
	if err := os.WriteFile(recurFile, []byte("recurFile Test"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	select {
	case msg := <-resChan:
		if msg.RelPath != "sub/test/deep/nop/recurFile.txt" {
			t.Errorf("Recursive watch: expected sub/test/deep/nop/recurFile.txt, got %s", msg.RelPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Recursive watch timed out")
	}
}
