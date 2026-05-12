package protocol

import "fmt"

type MessageType uint8

const (
	MsgNotify      MessageType = iota // 通知变动
	MsgPullRequest                    // 请求下载
	MsgFileData                       // 传输数据
	MsgeError                         // 错误信息
)

type SyncMessage struct {
	Type    MessageType `json:"type"`
	RelPath string      `json:"rel_path"`
	Hash    string      `json:"hash,omitempty"`
	Size    int64       `json:"size,omitempty"`
	ModTime int64       `json:"mod_time,omitempty"`
	Payload []byte      `json:"payload,omitempty"`
}

// 构造String转化成文字流
func (m SyncMessage) String() string {
	return fmt.Sprintf("Type: %d | Path: %s | Hash: %s", m.Type, m.RelPath, m.Hash)
}
