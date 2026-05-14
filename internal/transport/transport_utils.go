package transport

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/tuxnode/LanSync/internal/protocol"
)

const (
	Accept = "accept"
	Dial   = "dial"
)

/*
NewUUID 生成一个嵌入毫秒时间戳的 UUID（类 UUID v7）。
前 6 字节编码 time.Now().UnixMilli()，后续字节来自 crypto/rand。
时间戳嵌入确保了 PeerID 之间的全序关系，用于握手时的连接裁决。
*/
func NewUUID() string {
	var value [16]byte

	ms := uint64(time.Now().UnixMilli())

	var tsBytes [8]byte
	binary.BigEndian.PutUint64(tsBytes[:], ms)
	copy(value[0:6], tsBytes[2:8])

	if _, err := rand.Read(value[6:]); err != nil {
		return ""
	}

	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

// accessLoop 循环接受 TCP 连接，为每个连接启动独立的 goroutine。
// 调用方须通过 t.wg 跟踪此 goroutine 的生命周期。
func (t *tcpTransport) accessLoop() {
	defer t.listener.Close()

	for {
		conn, err := t.listener.Accept()
		if err != nil {
			log.Printf("[Transport] 监听停止: %v", err)
			return
		}

		log.Printf("[Transport] 收到来自 %s 的新连接", conn.RemoteAddr().String())

		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			t.handleConn(conn, Accept)
		}()
	}
}

// handleConn 处理单条 TCP 连接的生命周期：
// 握手 → 裁决 → 消息收发循环 → 清理。
func (t *tcpTransport) handleConn(conn net.Conn, direction string) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	myHandshake := protocol.SyncMessage{
		Type:      protocol.MsgHandShake,
		PeerID:    t.myID,
		Timestamp: time.Now().UnixMilli(),
	}

	var remotePeerID string

	if direction == Dial {
		if err := encoder.Encode(myHandshake); err != nil {
			return
		}
		var remoteMsg protocol.SyncMessage
		if err := decoder.Decode(&remoteMsg); err != nil || remoteMsg.Type != protocol.MsgHandShake {
			return
		}
		remotePeerID = remoteMsg.PeerID
	} else {
		var remoteMsg protocol.SyncMessage
		if err := decoder.Decode(&remoteMsg); err != nil || remoteMsg.Type != protocol.MsgHandShake {
			return
		}
		remotePeerID = remoteMsg.PeerID
		if err := encoder.Encode(myHandshake); err != nil {
			return
		}
	}

	// 裁决：获胜方应充当 Dialer，落败方应充当 Acceptor
	if t.myID < remotePeerID && direction == Accept ||
		t.myID > remotePeerID && direction == Dial {
		encoder.Encode(protocol.SyncMessage{Type: protocol.MsgHandShakeReject})
		return
	}

	// 去重：同一 PeerID 只允许保留一条连接。
	// 握手裁决保证了双方角色一致，但裁决无法防止同一 peer 先后发起多次连接。
	// 此处兜底：若 peers 中已存在该 remotePeerID，关闭当前连接。
	t.mu.Lock()
	if _, exists := t.peers[remotePeerID]; exists {
		t.mu.Unlock()
		encoder.Encode(protocol.SyncMessage{Type: protocol.MsgHandShakeReject})
		return
	}
	t.peers[remotePeerID] = &peerConn{conn: conn, peerID: remotePeerID, direction: direction}
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.peers, remotePeerID)
		t.mu.Unlock()
	}()

	conn.SetDeadline(time.Time{})
	for {
		var msg protocol.SyncMessage
		if err := decoder.Decode(&msg); err != nil {
			return
		}
		t.dispatchMsg(&msg, remotePeerID)
	}
}

func (t *tcpTransport) dispatchMsg(msg *protocol.SyncMessage, remotePeerID string) {
	switch msg.Type {
	case protocol.MsgNotify:
		if t.onMessage != nil {
			t.onMessage(remotePeerID, *msg)
		}
	case protocol.MsgPullRequest:
		if t.onMessage != nil {
			t.onMessage(remotePeerID, *msg)
		}
	case protocol.MsgFileData:
		if t.onMessage != nil {
			t.onMessage(remotePeerID, *msg)
		}
	case protocol.MsgError:
		log.Printf("[Transport] 来自 %s 的错误", remotePeerID)
	case protocol.MsgHandShake:
		// 已握手完成，忽略重入
	case protocol.MsgHandShakeReject:
		// 对方裁决落败放弃，关闭连接
	}
}
