package transport_test

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
	"github.com/tuxnode/LanSync/internal/transport"
)

func TestNewTransport(t *testing.T) {
	tr1 := transport.NewTransport(8888)
	tr2 := transport.NewTransport(9999)

	if tr1.MyID() == tr2.MyID() {
		t.Errorf("NewTransport: UUID 构造出现相同")
	}

	if err := tr1.Start(); err != nil {
		t.Fatalf("NewTransport: 启动失败 %v", err)
	}
	defer tr1.Stop()
	if tr1.Port() != 8888 {
		t.Errorf("NewTransport: tr1 端口期望 8888，实际 %d", tr1.Port())
	}

	if err := tr2.Start(); err != nil {
		t.Fatalf("NewTransport: 启动失败 %v", err)
	}
	defer tr2.Stop()
	if tr2.Port() != 9999 {
		t.Errorf("NewTransport: tr2 端口期望 9999，实际 %d", tr2.Port())
	}

	tr3 := transport.NewTransport(0)
	if err := tr3.Start(); err != nil {
		t.Fatal(err)
	}
	defer tr3.Stop()
	if tr3.Port() <= 0 {
		t.Errorf("NewTransport: 构造端口为0的时候，端口小于等于0")
	}
}

func TestServer(t *testing.T) {
	testTrans := transport.NewTransport(0)
	if err := testTrans.Start(); err != nil {
		t.Fatalf("TestServer: 无法启动服务 %v", err)
	}
	defer testTrans.Stop()

	// 检查端口是否可达
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", testTrans.Port()), time.Second)
	if err != nil {
		t.Fatalf("TestServer: 无法创建连接 %v", err)
	}
	conn.Close()

	// 测试占用端口后是否会返回error
	port := 19999
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("TestServer: 无法启动端口占用 %v", err)
	}
	portTrans := transport.NewTransport(port)
	if err := portTrans.Start(); err == nil {
		t.Errorf("TestServer: 启动被占用的端口期望返回error")
	}
	l.Close()
}

func TestStop(t *testing.T) {
	tr := transport.NewTransport(0)
	if err := tr.Start(); err != nil {
		t.Fatalf("TestStop: 无法启动服务 %v", err)
	}
	port := tr.Port()

	if err := tr.Stop(); err != nil {
		t.Fatalf("TestStop: 无法停止服务 %v", err)
	}

	_, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err == nil {
		t.Errorf("TestStop: 服务仍可连接，期望无法连接")
	}

	if tr.PeerCount() != 0 {
		t.Errorf("TestStop: 期望此时连接计数为0")
	}

	if err := tr.Stop(); err == nil {
		t.Errorf("TestStop: 两次Stop没有返回error")
	}

	tra := transport.NewTransport(0)
	if err := tra.Stop(); err == nil {
		t.Errorf("TestStop: 未Start直接Stop未触发error")
	}
}

func TestConnect(t *testing.T) {
	tr := transport.NewTransport(0)

	if err := tr.Start(); err != nil {
		t.Fatalf("TestConnect: 启动服务失败 %v", err)
	}
	defer tr.Stop()

	// 传递错误的地址
	if err := tr.ConnectTo("Invalid Addr"); err == nil {
		t.Errorf("TestConnect: 传递无效地址未返回error，期望接收到error")
	}

	// 测试不可达地址
	if err := tr.ConnectTo("192.0.2.1:9032"); err == nil {
		t.Errorf("TestConnect: 期望返回 error")
	}

	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()
	addr := fmt.Sprintf("127.0.0.1:%d", trB.Port())

	// A 连上 B
	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	if err := trA.ConnectTo(addr); err != nil {
		t.Fatal("首次连接失败:", err)
	}

	// 第二次连接同一地址 → 握手时 B 发现 PeerID 重复，返回 MsgHandShakeReject
	if err := trA.ConnectTo(addr); err == nil {
		t.Fatal("重复连接应返回 error")
	}
}

func TestPeersAndPeerCount(t *testing.T) {
	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()

	addr := fmt.Sprintf("127.0.0.1:%d", trB.Port())

	// 初始无 peer
	if trB.PeerCount() != 0 {
		t.Errorf("PeerCount 初始应为 0，实际 %d", trB.PeerCount())
	}
	if len(trB.Peers()) != 0 {
		t.Errorf("Peers 初始应为空，实际 %v", trB.Peers())
	}

	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	if err := trA.ConnectTo(addr); err != nil {
		t.Fatal("首次连接失败:", err)
	}

	// 双方各有 1 个 peer
	if trA.PeerCount() != 1 {
		t.Errorf("A PeerCount 期望 1，实际 %d", trA.PeerCount())
	}
	if trB.PeerCount() != 1 {
		t.Errorf("B PeerCount 期望 1，实际 %d", trB.PeerCount())
	}

	// Peers 返回对方 PeerID
	if trA.Peers()[0] != trB.MyID() {
		t.Errorf("A 的 peer 应为 B 的 ID")
	}
	if trB.Peers()[0] != trA.MyID() {
		t.Errorf("B 的 peer 应为 A 的 ID")
	}

	// 断开后计数归零
	trA.Stop()
	time.Sleep(50 * time.Millisecond) // 等待 B 侧 readLoop 感知断连

	if trB.PeerCount() != 0 {
		t.Errorf("A 断开后 B.PeerCount 期望 0，实际 %d", trB.PeerCount())
	}
}

func TestSendTo(t *testing.T) {
	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()

	addr := fmt.Sprintf("127.0.0.1:%d", trB.Port())

	// B 注册回调
	msgCh := make(chan string, 1)
	trB.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		msgCh <- msg.RelPath
	})

	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	if err := trA.ConnectTo(addr); err != nil {
		t.Fatal("连接失败:", err)
	}

	// A 向 B 发送消息
	testMsg := protocol.SyncMessage{
		Type:    protocol.MsgNotify,
		RelPath: "test/send.txt",
	}
	if err := trA.SendTo(trB.MyID(), testMsg); err != nil {
		t.Fatal("SendTo 失败:", err)
	}

	select {
	case path := <-msgCh:
		if path != "test/send.txt" {
			t.Errorf("期望路径 test/send.txt，实际 %s", path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnMessage 回调未在 2s 内触发")
	}

	// 发送给不存在的 peer
	if err := trA.SendTo("nonexistent-id", testMsg); err == nil {
		t.Errorf("发送给不存在 peer 应返回 error")
	}
}

func TestBroadcast(t *testing.T) {
	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()

	trC := transport.NewTransport(0)
	if err := trC.Start(); err != nil {
		t.Fatal(err)
	}
	defer trC.Stop()

	addrB := fmt.Sprintf("127.0.0.1:%d", trB.Port())
	addrC := fmt.Sprintf("127.0.0.1:%d", trC.Port())

	msgCh := make(chan string, 2)
	trB.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		msgCh <- msg.RelPath
	})
	trC.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		msgCh <- msg.RelPath
	})

	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	if err := trA.ConnectTo(addrB); err != nil {
		t.Fatal("连接 B 失败:", err)
	}
	if err := trA.ConnectTo(addrC); err != nil {
		t.Fatal("连接 C 失败:", err)
	}

	// 广播消息
	trA.Broadcast(protocol.SyncMessage{
		Type:    protocol.MsgNotify,
		RelPath: "broadcast/file.txt",
	})

	received := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case path := <-msgCh:
			received[path] = true
		case <-time.After(2 * time.Second):
			t.Fatal("Broadcast 回调未在 2s 内全部收到")
		}
	}
	if !received["broadcast/file.txt"] || len(received) != 1 {
		t.Errorf("Broadcast 期望所有 peer 收到 'broadcast/file.txt'，收到 %v", received)
	}
}

func TestOnMessage(t *testing.T) {
	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()

	addr := fmt.Sprintf("127.0.0.1:%d", trB.Port())

	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	// 在连接前注册回调
	msgCh := make(chan struct {
		peerID string
		msg    protocol.SyncMessage
	}, 3)
	trB.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		msgCh <- struct {
			peerID string
			msg    protocol.SyncMessage
		}{peerID, msg}
	})

	if err := trA.ConnectTo(addr); err != nil {
		t.Fatal("连接失败:", err)
	}

	// 发送多种类型的消息
	for _, msgType := range []protocol.MessageType{
		protocol.MsgNotify,
		protocol.MsgPullRequest,
		protocol.MsgError,
	} {
		trA.SendTo(trB.MyID(), protocol.SyncMessage{
			Type:    msgType,
			RelPath: "test",
			Hash:    "abc123",
		})
	}

	for i := 0; i < 3; i++ {
		select {
		case result := <-msgCh:
			if result.peerID != trA.MyID() {
				t.Errorf("peerID 应为 %s，实际 %s", trA.MyID(), result.peerID)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("期望 3 条消息，只收到 %d 条", i)
		}
	}

	// nil 回调不 panic
	trC := transport.NewTransport(0)
	if err := trC.Start(); err != nil {
		t.Fatal(err)
	}
	defer trC.Stop()
	trC.Broadcast(protocol.SyncMessage{Type: protocol.MsgNotify})
}

func TestHandshakeReject(t *testing.T) {
	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()

	addrB := fmt.Sprintf("127.0.0.1:%d", trB.Port())

	// A 连 B：首次连接成功
	if err := trA.ConnectTo(addrB); err != nil {
		t.Fatal("首次连接失败:", err)
	}

	// A 再次连 B → B 检测到重复 PeerID，握手返回 error
	if err := trA.ConnectTo(addrB); err == nil {
		t.Fatal("重复连接应被拒绝")
	}

	// 验证 B 侧只有一个连接
	if trB.PeerCount() != 1 {
		t.Errorf("B 应只有 1 个 peer，实际 %d", trB.PeerCount())
	}
}

func TestConnectToSelf(t *testing.T) {
	tr := transport.NewTransport(0)
	if err := tr.Start(); err != nil {
		t.Fatal(err)
	}
	defer tr.Stop()

	// 连接自身
	err := tr.ConnectTo(fmt.Sprintf("127.0.0.1:%d", tr.Port()))
	if err == nil {
		t.Fatal("连接自身应返回 error（握手时 MyID == PeerID 或裁决落败）")
	}
}

func TestConcurrentConnect(t *testing.T) {
	// 多个 Transport 同时连接到同一个目标
	server := transport.NewTransport(0)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	addr := fmt.Sprintf("127.0.0.1:%d", server.Port())

	n := 10
	errCh := make(chan error, n)
	var clientsMu sync.Mutex
	var clients []transport.Transport

	for i := 0; i < n; i++ {
		go func() {
			tr := transport.NewTransport(0)
			if err := tr.Start(); err != nil {
				errCh <- err
				return
			}
			clientsMu.Lock()
			clients = append(clients, tr)
			clientsMu.Unlock()
			errCh <- tr.ConnectTo(addr)
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("并发连接 %d 失败: %v", i, err)
		}
	}

	if server.PeerCount() != n {
		t.Errorf("服务端 PeerCount 期望 %d，实际 %d", n, server.PeerCount())
	}

	// 清理所有客户端
	for _, c := range clients {
		c.Stop()
	}
}
