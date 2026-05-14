package transport

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
)

/*
Transport 定义了网络层的公共API。
上层模块通过逻辑 PeerID 与对等节点交互，不接触原始 net.Conn。

基于时间戳的握手机制：
每个 Transport 实例在创建时通过 NewUUID() 生成 PeerID，
UUID 前6字节编码了毫秒级时间戳，天然保证全序关系。
当两个 peer 之间出现重复 TCP 连接时，PeerID 较小的一方
总是充当 Dialer（主动连接方），较大的一方总是充当
Listener（被动接收方），不符合此角色关系的连接将被关闭。
*/
type Transport interface {
	// Start 在配置的端口上开始监听,启动 accept 循环。
	Start() error

	// Stop 优雅关闭监听器和所有 peer 连接。
	Stop() error

	// ConnectTo 向 addr 发起 TCP 连接，完成握手后按 PeerID 注册 peer。
	// 若握手超时、对方拒绝或裁决落败，返回 error 并关闭连接。
	ConnectTo(addr string) error

	// SendTo 向指定 PeerID 的对等节点发送消息。
	SendTo(peerID string, msg protocol.SyncMessage) error

	// Broadcast 向所有已连接的 peer 广播消息。
	Broadcast(msg protocol.SyncMessage) error

	// OnMessage 注册消息回调，每条来自 peer 的消息都会调用 handler。
	// handler 的第一个参数是发送者的 PeerID。
	OnMessage(handler func(peerID string, msg protocol.SyncMessage))

	// Peers 返回当前所有已连接 peer 的 PeerID 快照。
	Peers() []string

	// PeerCount 返回已连接 peer 数量。
	PeerCount() int

	// Port 返回 Transport 实际监听的端口。
	// 当 NewTransport(0) 使用系统分配端口时，Start() 后会更新。
	Port() int

	// MyID 返回本节点的唯一标识。
	MyID() string
}

// peerConn 包装原始 TCP 连接及其握手元数据。
type peerConn struct {
	conn      net.Conn // 原始 TCP 连接
	peerID    string   // 对端 PeerID
	direction string   // "dial"（我方主动发起）或 "accept"（我方被动接收）
}

// tcpTransport 是 Transport 接口的 TCP 实现。
type tcpTransport struct {
	port     int
	listener net.Listener
	mu       sync.RWMutex
	peers    map[string]*peerConn // key = 对端 PeerID
	myID     string

	onMessage func(peerID string, msg protocol.SyncMessage)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewTransport 创建一个具有唯一标识的 Transport 实例。
// PeerID 通过 NewUUID() 生成，UUID 中嵌入毫秒时间戳用于握手裁决。
func NewTransport(port int) Transport {
	ctx, cancel := context.WithCancel(context.Background())
	return &tcpTransport{
		port:   port,
		peers:  make(map[string]*peerConn),
		myID:   NewUUID(),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (t *tcpTransport) Start() error {
	addr := fmt.Sprintf(":%d", t.port)

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("Transport: Listen on %s %v", addr, err)
	}

	t.listener = l
	if t.port == 0 {
		t.port = l.Addr().(*net.TCPAddr).Port
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.accessLoop()
	}()

	return nil
}

func (t *tcpTransport) Stop() error {
	if t.listener == nil {
		return fmt.Errorf("transport: Stop before Start, or already stopped")
	}

	t.listener.Close()

	t.mu.Lock()
	for _, pc := range t.peers {
		pc.conn.Close()
	}
	t.mu.Unlock()

	t.wg.Wait()

	t.mu.Lock()
	t.peers = make(map[string]*peerConn)
	t.mu.Unlock()

	t.listener = nil
	return nil
}

func (t *tcpTransport) ConnectTo(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("transport: ConnectTo %s: %w", addr, err)
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.handleConn(conn, Dial)
	}()

	return nil
}

func (t *tcpTransport) SendTo(peerID string, msg protocol.SyncMessage) error {
	return fmt.Errorf("transport: SendTo not implemented")
}

func (t *tcpTransport) Broadcast(msg protocol.SyncMessage) error {
	return fmt.Errorf("transport: Broadcast not implemented")
}

func (t *tcpTransport) OnMessage(handler func(peerID string, msg protocol.SyncMessage)) {
	t.mu.Lock()
	t.onMessage = handler
	t.mu.Unlock()
}

func (t *tcpTransport) Peers() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ids := make([]string, 0, len(t.peers))
	for id := range t.peers {
		ids = append(ids, id)
	}
	return ids
}

func (t *tcpTransport) PeerCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.peers)
}

func (t *tcpTransport) Port() int {
	return t.port
}

func (t *tcpTransport) MyID() string {
	return t.myID
}
