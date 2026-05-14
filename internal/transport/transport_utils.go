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

// handshake 执行握手协议：交换 PeerID → 裁决 → 去重 → 注册。
// 成功时返回对方 PeerID，调用方负责后续消息循环和连接清理。
func (t *tcpTransport) handshake(conn net.Conn, direction string) (string, error) {
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
			return "", fmt.Errorf("handshake: send my handshake: %w", err)
		}

		var remoteMsg protocol.SyncMessage
		if err := decoder.Decode(&remoteMsg); err != nil {
			return "", fmt.Errorf("handshake: receive remote handshake: %w", err)
		}

		// 对方握手失败，发来拒绝
		if remoteMsg.Type == protocol.MsgHandShakeReject {
			return "", fmt.Errorf("handshake: remote rejected")
		}
		if remoteMsg.Type != protocol.MsgHandShake {
			return "", fmt.Errorf("handshake: expected Handshake, got type=%d", remoteMsg.Type)
		}
		remotePeerID = remoteMsg.PeerID
	} else {
		var remoteMsg protocol.SyncMessage
		if err := decoder.Decode(&remoteMsg); err != nil {
			return "", fmt.Errorf("handshake: receive remote handshake: %w", err)
		}
		if remoteMsg.Type != protocol.MsgHandShake {
			return "", fmt.Errorf("handshake: expected Handshake, got type=%d", remoteMsg.Type)
		}
		remotePeerID = remoteMsg.PeerID

		if err := encoder.Encode(myHandshake); err != nil {
			return "", fmt.Errorf("handshake: send my handshake: %w", err)
		}
	}

	// 注册 + 去重裁决。
	// 首次连接直接注册；若已有同 PeerID 的连接，分两种情况：
	//   - 同向重复（两次 dial 或两次 accept）：直接拒绝新连接
	//   - 双向重复（一方 dial 一方 accept）：按 ID 角色保留正确的那条
	t.mu.Lock()
	existing, exists := t.peers[remotePeerID]
	if exists {
		if existing.direction == direction {
			// 同向重复连接，保留已有，拒绝当前
			t.mu.Unlock()
			encoder.Encode(protocol.SyncMessage{Type: protocol.MsgHandShakeReject})
			return "", fmt.Errorf("handshake: duplicate peer %s", remotePeerID)
		}

		// 双向连接裁决：PeerID 较小者充当 Dialer，较大者充当 Acceptor。
		// 比较当前连接与已有连接的方向，保留角色正确的那条。
		keepExisting := false
		if direction == Dial {
			// 当前是 Dialer → 较小 ID 应保留 Dial 连接
			if t.myID < remotePeerID {
				existing.conn.Close()
				t.peers[remotePeerID] = &peerConn{conn: conn, peerID: remotePeerID, direction: direction}
			} else {
				keepExisting = true
			}
		} else {
			// 当前是 Acceptor → 较大 ID 应保留 Accept 连接
			if t.myID > remotePeerID {
				existing.conn.Close()
				t.peers[remotePeerID] = &peerConn{conn: conn, peerID: remotePeerID, direction: direction}
			} else {
				keepExisting = true
			}
		}
		t.mu.Unlock()
		if keepExisting {
			encoder.Encode(protocol.SyncMessage{Type: protocol.MsgHandShakeReject})
			return "", fmt.Errorf("handshake: duplicate, keeping existing conn")
		}
		return remotePeerID, nil
	}
	// 首次连接，直接注册
	t.peers[remotePeerID] = &peerConn{conn: conn, peerID: remotePeerID, direction: direction}
	t.mu.Unlock()

	return remotePeerID, nil
}

// readLoop 循环读取消息并分发给上层回调，直到连接断开。
// 调用方负责通过 wg 跟踪此 goroutine 的生命周期。
func (t *tcpTransport) readLoop(conn net.Conn, remotePeerID string) {
	defer conn.Close()
	defer func() {
		t.mu.Lock()
		delete(t.peers, remotePeerID)
		t.mu.Unlock()
	}()

	conn.SetDeadline(time.Time{})
	decoder := json.NewDecoder(conn)

	for {
		var msg protocol.SyncMessage
		if err := decoder.Decode(&msg); err != nil {
			return
		}
		t.dispatchMsg(&msg, remotePeerID)
	}
}

// handleConn 处理单条 TCP 连接的生命周期：
// 握手 → 裁决 → 消息收发循环 → 清理。
func (t *tcpTransport) handleConn(conn net.Conn, direction string) {
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	remotePeerID, err := t.handshake(conn, direction)
	if err != nil {
		conn.Close()
		log.Printf("[Transport] 握手失败: %v", err)
		return
	}

	t.readLoop(conn, remotePeerID)
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
		if t.onMessage != nil {
			t.onMessage(remotePeerID, *msg)
		}
	case protocol.MsgHandShake:
		// 已握手完成，忽略重入
	case protocol.MsgHandShakeReject:
		// 对方裁决落败放弃，关闭连接
	}
}
