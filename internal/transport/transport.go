package transport

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
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
	for host, conn := range t.peer {
		err := t.SendMessage(conn, msg)
		if err != nil {
			return fmt.Errorf("Fail to BroadCast: %v", err)
		}
		log.Printf("SendMessage To Host: %s", host)
	}
	return nil
}

// 接收Message并分配goroutine
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
		go t.handleEachConn()
	}
}

// TODO:为每个连接分配goroutine
func (t *Transport) handleEachConn() {}
