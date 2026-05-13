package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
)

type Transport struct {
	Port   int
	listen net.Listener
	mu     sync.RWMutex
	peer   map[string]*net.Conn
}

func NewTransport(port int) *Transport {
	return &Transport{
		Port: port,
		peer: make(map[string]*net.Conn),
	}
}

func (t *Transport) SendMessage(conn *net.Conn, msg protocol.SyncMessage) error {
	if conn == nil {
		return fmt.Errorf("Connection is nil")
	}

	// 设置最低时限
	(*conn).SetDeadline(time.Now().Add(5 * time.Second))

	err := json.NewEncoder(*conn).Encode(msg)
	if err != nil {
		return fmt.Errorf("Json Encode Message Failt")
	}

	return nil
}

func (t *Transport) BroadCast(msg protocol.SyncMessage) error {
	t.mu.RLock()

	// 复制一份避免长连接，单独处理广播
	conns := make(map[string]*net.Conn)
	for h, c := range t.peer {
		conns[h] = c
	}
	t.mu.RUnlock()

	for host, conn := range conns {
		_ = t.SendMessage(conn, msg)
		log.Printf("[Transport] 广播到: %s", host)
	}
	return nil
}

func (t *Transport) ConnectTo(addr string) error {
	t.mu.RLock()
	_, exits := t.peer[addr]
	t.mu.RUnlock()

	if exits {
		log.Printf("[Transport] 已经连接: %s", addr)
	}

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.peer[addr] = &conn
	t.mu.Unlock()

	// TODO:处理连接

	return nil
}

// 等待连接，接收Message，为每个连接分配goroutine
func (t *Transport) Start() error {
	addr := fmt.Sprintf(":%d", t.Port)

	// 开启Tcp Lisening
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("Fail to Creat TCP Lisening")
	}

	t.listen = l
	if t.Port == 0 {
		t.Port = l.Addr().(*net.TCPAddr).Port
	}

	log.Printf("[Transport] 服务已启动，监听地址 %s\n", l.Addr().String())

	go t.accessLoop()

	return nil
}

func (t *Transport) accessLoop() {
	defer t.listen.Close()

	for {
		conn, err := t.listen.Accept()
		if err != nil {
			log.Printf("[Transport] 监听停止: %v\n", err)
			return
		}
		log.Printf("[Transport] 收到来自 %s 的新连接\n", conn.RemoteAddr().String())

		t.mu.Lock()
		t.peer[conn.RemoteAddr().String()] = &conn
		t.mu.Unlock()

		// 为每个连接分配goroutine
		go t.handleEachConn(&conn)
	}
}

func (t *Transport) handleEachConn(conn *net.Conn) {
	defer func() {
		(*conn).Close()
		t.mu.Lock()
		delete(t.peer, (*conn).RemoteAddr().String())
		t.mu.Unlock()
	}()

	decoder := json.NewDecoder(*conn)

	for {
		var msg protocol.SyncMessage

		if err := decoder.Decode(&msg); err != nil {
			return
		}

		t.dispatchMsg(&msg, (*conn).RemoteAddr().String())
	}
}

// TODO:通知变动
func (t *Transport) handleNotify(msg *protocol.SyncMessage, remoteAddr string) error {
}

// TODO:实现Message路由逻辑
func (t *Transport) dispatchMsg(msg *protocol.SyncMessage, remoteAddr string) {
	switch msg.Type {
	case protocol.MsgNotify:
		if err := t.handleNotify(msg, remoteAddr); err != nil {
			return
		}
	}
}
