package transport

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
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
