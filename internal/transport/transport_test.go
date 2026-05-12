package transport

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
)

func TestTransport_Start(t testing.T) {
	port := 9999
	tr := NewTransport(port)

	err := tr.Start()
	if err != nil {
		t.Fatalf("Can't Start Listening: %v", err)
	}
	defer tr.listen.Close()

	// 尝试连接端口
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("Fail to Creat TCP Connect: %v", err)
	}
	conn.Close()
}

func TestTransport_SendMessage(t testing.T) {
	tr := NewTransport(0)

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	testMsg := protocol.SyncMessage{
		Type:    protocol.MsgNotify,
		RelPath: "test.txt",
	}

	go func() {
		err := tr.SendMessage(&client, testMsg)
		if err != nil {
			t.Errorf("Fail to Send Message: %v", err)
		}
	}()

	var received protocol.SyncMessage
	err := json.NewDecoder(server).Decode(&received)
	if err != nil {
		t.Errorf("Unable to Serialize Data: %v", err)
	}
	if received.RelPath != testMsg.RelPath {
		t.Errorf("Expect Path: %s, Fact Path: %s", testMsg.RelPath, received.RelPath)
	}
}
