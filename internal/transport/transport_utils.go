package transport

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"
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
			t.handleConn(conn)
		}()
	}
}

// handleConn 处理单条 TCP 连接的生命周期：
// 握手 → 裁决 → 消息收发循环 → 清理。
func (t *tcpTransport) handleConn(conn net.Conn) {
	defer conn.Close()

	// TODO: 握手 + 裁决 + 消息循环
	log.Printf("[Transport] 连接 %s 暂未实现处理逻辑，关闭", conn.RemoteAddr())
}
