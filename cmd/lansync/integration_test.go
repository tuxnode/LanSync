package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
	"github.com/tuxnode/LanSync/internal/transport"
	"github.com/tuxnode/LanSync/internal/watcher"
)

// testDaemon creates a minimal daemonState with real transport and watcher.
func testDaemon(t *testing.T, dir string) *daemonState {
	t.Helper()

	tr := transport.NewTransport(0)
	if err := tr.Start(); err != nil {
		t.Fatalf("transport start: %v", err)
	}
	t.Cleanup(func() { tr.Stop() })

	fsw, err := watcher.NewWatcher(dir)
	if err != nil {
		t.Fatalf("watcher create: %v", err)
	}
	fsw.WatcherStart()
	t.Cleanup(func() { fsw.WatcherStop() })

	ds := newDaemonState()
	ds.tr = tr
	ds.fsw = fsw
	ds.myID = tr.MyID()
	ds.watchDir = dir
	ds.port = tr.Port()
	ds.startTime = time.Now()

	return ds
}

func waitForPeers(t *testing.T, tr transport.Transport, expected int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d peers, have %d", expected, len(tr.Peers()))
		default:
			if len(tr.Peers()) >= expected {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for file: %s", path)
		default:
			if _, err := os.Stat(path); err == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestHTTPAPI tests all HTTP API endpoints via httptest.
func TestHTTPAPI(t *testing.T) {
	dir := t.TempDir()
	ds := testDaemon(t, dir)

	srv := httptest.NewServer(ds.httpHandler(9786))
	t.Cleanup(srv.Close)

	t.Run("status returns all fields", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		if body["my_id"] != ds.myID {
			t.Errorf("my_id = %v, want %v", body["my_id"], ds.myID)
		}
		if body["watch_dir"] != dir {
			t.Errorf("watch_dir = %v, want %v", body["watch_dir"], dir)
		}
		if body["port"] != float64(ds.port) {
			t.Errorf("port = %v, want %v", body["port"], ds.port)
		}
		if body["http_port"] != float64(9786) {
			t.Errorf("http_port = %v, want 9786", body["http_port"])
		}
		if body["peer_count"] != float64(0) {
			t.Errorf("peer_count = %v, want 0", body["peer_count"])
		}
		if body["connected"] != float64(0) {
			t.Errorf("connected = %v, want 0", body["connected"])
		}
		if _, ok := body["uptime"]; !ok {
			t.Error("uptime field missing")
		}
		if _, ok := body["start_time"]; !ok {
			t.Error("start_time field missing")
		}
	})

	t.Run("peers returns empty initially", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/peers")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		peers, ok := body["peers"].([]interface{})
		if !ok {
			t.Fatal("peers field is not an array")
		}
		if len(peers) != 0 {
			t.Errorf("peers length = %d, want 0", len(peers))
		}
	})

	t.Run("peers returns inserted peers in order", func(t *testing.T) {
		ds.upsertPeer("192.168.1.1:9876", "test-host", stFound)
		ds.upsertPeer("192.168.1.2:9876", "connected-host", stConnected)

		resp, err := http.Get(srv.URL + "/api/peers")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		peers, ok := body["peers"].([]interface{})
		if !ok {
			t.Fatal("peers field is not an array")
		}
		if len(peers) != 2 {
			t.Fatalf("peers length = %d, want 2", len(peers))
		}

		p0 := peers[0].(map[string]interface{})
		p1 := peers[1].(map[string]interface{})
		if p0["addr"] != "192.168.1.1:9876" {
			t.Errorf("peers[0].addr = %v", p0["addr"])
		}
		if p0["hostname"] != "test-host" {
			t.Errorf("peers[0].hostname = %v", p0["hostname"])
		}
		if p1["addr"] != "192.168.1.2:9876" {
			t.Errorf("peers[1].addr = %v", p1["addr"])
		}
		if p1["status"] != "已连接" {
			t.Errorf("peers[1].status = %v", p1["status"])
		}
	})

	t.Run("log returns entries", func(t *testing.T) {
		ds.addLog("test info message", "info")
		ds.addLog("test sync message", "sync")
		ds.addLog("test error message", "err")

		resp, err := http.Get(srv.URL + "/api/log")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		logs, ok := body["logs"].([]interface{})
		if !ok {
			t.Fatal("logs field is not an array")
		}
		if len(logs) < 3 {
			t.Fatalf("logs length = %d, want >= 3", len(logs))
		}

		last := logs[len(logs)-1].(map[string]interface{})
		if last["content"] != "test error message" {
			t.Errorf("last log content = %v, want 'test error message'", last["content"])
		}
		if last["level"] != "err" {
			t.Errorf("last log level = %v, want 'err'", last["level"])
		}
		if _, ok := last["time"]; !ok {
			t.Error("log entry missing time field")
		}
	})

	t.Run("index returns file count", func(t *testing.T) {
		testFile := filepath.Join(dir, "index_test.txt")
		if err := os.WriteFile(testFile, []byte("index test content"), 0644); err != nil {
			t.Fatal(err)
		}

		resp, err := http.Get(srv.URL + "/api/index")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}

		count, ok := body["count"].(float64)
		if !ok {
			t.Fatal("count field missing or not a number")
		}
		if count != 1 {
			t.Errorf("count = %v, want 1", count)
		}
		if _, ok := body["elapsed"]; !ok {
			t.Error("elapsed field missing")
		}
	})

	t.Run("connect rejects GET", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/connect")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", resp.StatusCode)
		}
	})

	t.Run("connect rejects missing addr", func(t *testing.T) {
		resp, err := http.Post(srv.URL+"/api/connect", "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("connect triggers peer connection", func(t *testing.T) {
		ds2 := testDaemon(t, t.TempDir())
		addr := fmt.Sprintf("127.0.0.1:%d", ds2.port)

		body := fmt.Sprintf(`{"addr":"%s"}`, addr)
		resp, err := http.Post(srv.URL+"/api/connect", "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result["status"] != "ok" {
			t.Errorf("status field = %v, want 'ok'", result["status"])
		}
		if result["addr"] != addr {
			t.Errorf("addr field = %v, want %s", result["addr"], addr)
		}

		waitForPeers(t, ds.tr, 1)
	})
}

// TestTwoPeerFileSync tests the complete file sync flow between two daemon instances.
func TestTwoPeerFileSync(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	dsA := testDaemon(t, dirA)
	dsB := testDaemon(t, dirB)

	// Wire message handlers (same as runDaemon)
	dsA.tr.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		switch msg.Type {
		case protocol.MsgNotify:
			dsA.addLog(fmt.Sprintf("← %s notify: %s", shortStr(peerID, 10), msg.RelPath), "sync")
			go dsA.handleRecvNotify(peerID, msg)
		case protocol.MsgPullRequest:
			dsA.addLog(fmt.Sprintf("← %s pull: %s", shortStr(peerID, 10), msg.RelPath), "info")
			go dsA.handleRecvPullRequest(peerID, msg)
		case protocol.MsgFileData:
			dsA.addLog(fmt.Sprintf("← %s data: %s", shortStr(peerID, 10), msg.RelPath), "sync")
			go dsA.handleRecvFileData(msg)
		}
	})
	dsA.fsw.OnMessage = func(msg protocol.SyncMessage) {
		dsA.tr.Broadcast(msg)
	}

	dsB.tr.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		switch msg.Type {
		case protocol.MsgNotify:
			dsB.addLog(fmt.Sprintf("← %s notify: %s", shortStr(peerID, 10), msg.RelPath), "sync")
			go dsB.handleRecvNotify(peerID, msg)
		case protocol.MsgPullRequest:
			dsB.addLog(fmt.Sprintf("← %s pull: %s", shortStr(peerID, 10), msg.RelPath), "info")
			go dsB.handleRecvPullRequest(peerID, msg)
		case protocol.MsgFileData:
			dsB.addLog(fmt.Sprintf("← %s data: %s", shortStr(peerID, 10), msg.RelPath), "sync")
			go dsB.handleRecvFileData(msg)
		}
	})
	dsB.fsw.OnMessage = func(msg protocol.SyncMessage) {
		dsB.tr.Broadcast(msg)
	}

	// Connect B -> A
	addrA := fmt.Sprintf("127.0.0.1:%d", dsA.port)
	if err := dsB.tr.ConnectTo(addrA); err != nil {
		t.Fatalf("connect B -> A: %v", err)
	}

	waitForPeers(t, dsA.tr, 1)
	waitForPeers(t, dsB.tr, 1)

	// Write a file in A's watch directory
	content := []byte("Hello from LanSync integration test!")
	testFile := filepath.Join(dirA, "sync_test.txt")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for the file to appear in B's directory (synced via transport)
	destFile := filepath.Join(dirB, "sync_test.txt")
	waitForFile(t, destFile, 10*time.Second)

	// Verify content matches
	got, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("read synced file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("synced content = %q, want %q", string(got), string(content))
	}

	expectedLog := fmt.Sprintf("✓ 文件已同步: sync_test.txt (%d bytes)", len(content))
	dsB.mu.RLock()
	var found bool
	for _, entry := range dsB.logs {
		if entry.Content == expectedLog {
			found = true
			break
		}
	}
	dsB.mu.RUnlock()
	if !found {
		t.Errorf("B's logs did not contain sync confirmation, want: %s", expectedLog)
	}
}

// TestDaemonLifecycle tests full daemon start and graceful shutdown via signal.
func TestDaemonLifecycle(t *testing.T) {
	dir := t.TempDir()

	done := make(chan struct{})
	go func() {
		runDaemon([]string{
			"--dir", dir,
			"--port", "0",
			"--http", "29876",
		})
		close(done)
	}()

	// Wait for HTTP API to become available
	var httpReady bool
	for i := 0; i < 50; i++ {
		resp, err := http.Get("http://127.0.0.1:29876/api/status")
		if err == nil {
			resp.Body.Close()
			httpReady = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !httpReady {
		t.Fatal("daemon HTTP API not ready within 5 seconds")
	}

	// Verify status API returns correct data
	resp, err := http.Get("http://127.0.0.1:29876/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		resp.Body.Close()
		t.Fatal(err)
	}
	resp.Body.Close()

	if status["watch_dir"] != dir {
		t.Errorf("watch_dir = %v, want %v", status["watch_dir"], dir)
	}
	if _, ok := status["my_id"]; !ok {
		t.Error("my_id field missing")
	}
	if _, ok := status["port"]; !ok {
		t.Error("port field missing")
	}
	if _, ok := status["uptime"]; !ok {
		t.Error("uptime field missing")
	}

	// Send SIGINT to trigger graceful shutdown
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("kill SIGINT: %v", err)
	}

	// Wait for daemon to exit
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not shut down within 5 seconds")
	}
}
