package transport

import (
	"net"
	"sync"

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
	return nil
}

func (t *Transport) BroadCast(msg protocol.SyncMessage) error {
	return nil
}

func (t *Transport) Start() error {
	return nil
}
