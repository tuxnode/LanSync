package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
)

func TestWatcherBasic(t *testing.T) {
	tempDir := t.TempDir()

	watcher := NewWatcher(tempDir)

	// 创建一个chan来接受Message
	resChan := make(chan protocol.SyncMessage, 1)

	watcher.OnMessage = func(msg protocol.SyncMessage) {
		resChan <- msg
	}

	// 启动监视器
	watcher.WatcherStart()
	time.Sleep(50 * time.Millisecond)

	// 测试WriteFile
	tempData := "Test WriteFile"
	testFile := filepath.Join(tempDir, "test.txt")

	// 测试是否成功忽略
	ignoreFile := filepath.Join(tempDir, "ignore.txt")
	watcher.AddIgnorePath(ignoreFile)

	// 触发写入
	erro := os.WriteFile(ignoreFile, []byte("I'm from network"), 0644)
	if erro != nil {
		t.Fatalf("WriteFile Test Fail: %v", erro)
	}
	err := os.WriteFile(testFile, []byte(tempData), 0644)
	if err != nil {
		t.Fatalf("WriteFile Test Fail: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	select {
	case msg := <-resChan:
		if msg.RelPath != "test.txt" {
			t.Errorf("Path Check Fail: %s", msg.RelPath)
		}
		if msg.Type != protocol.MsgNotify {
			t.Errorf("Message type Fail: %v", msg.Type)
		}
		t.Logf("Success: Receive The Message: %v", msg)
	case <-time.After(2 * time.Second):
		t.Fatalf("WriteFile Test Time out")
	}

	select {
	case msg := <-resChan:
		if msg.RelPath == "ignore.txt" {
			t.Errorf("Ignore File Test Fail: %v", msg.RelPath)
		}
	default:
		t.Logf("Success: Test Ignore File")
	}

	os.Remove(testFile)
	os.Remove(ignoreFile)
}
