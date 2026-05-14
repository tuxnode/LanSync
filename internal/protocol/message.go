package protocol

import "fmt"

type MessageType uint8

const (
	MsgNotify          MessageType = iota // 通知变动
	MsgPullRequest                        // 请求下载
	MsgFileData                           // 传输数据
	MsgError                              // 错误信息
	MsgHandShake                          // 握手
	MsgHandShakeReject                    // 握手拒绝（裁决落败）
)

type SyncMessage struct {
	Type      MessageType `json:"type"`
	RelPath   string      `json:"rel_path,omitempty"`
	Hash      string      `json:"hash,omitempty"`
	Size      int64       `json:"size,omitempty"`
	ModTime   int64       `json:"mod_time,omitempty"`
	PeerID    string      `json:"peer_id,omitempty"`
	Timestamp int64       `json:"timestamp,omitempty"`
}

func (m SyncMessage) String() string {
	return fmt.Sprintf("Type:%d Path:%s Hash:%s Size:%d ModTime:%d PeerID:%s Timestamp:%d",
		m.Type, m.RelPath, m.Hash, m.Size, m.ModTime, m.PeerID, m.Timestamp)
}
