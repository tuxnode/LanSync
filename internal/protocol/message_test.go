package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageTypeValues(t *testing.T) {
	// MsgNotify 为 0 时存在零值歧义，这里只验证常量互不相等
	types := []MessageType{
		MsgNotify, MsgPullRequest, MsgFileData,
		MsgError, MsgHandShake, MsgHandShakeReject,
	}
	seen := make(map[MessageType]bool)
	for _, typ := range types {
		if seen[typ] {
			t.Errorf("消息类型 %d 重复定义", typ)
		}
		seen[typ] = true
	}
}

func TestSyncMessageJSONRoundTrip(t *testing.T) {
	original := SyncMessage{
		Type:      MsgNotify,
		RelPath:   "docs/readme.md",
		Hash:      "abc123def456",
		Size:      1024,
		ModTime:   1715690000,
		PeerID:    "01926b00-0000-7000-8000-000000000001",
		Timestamp: 1715690000000,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded SyncMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type: 期望 %d, 实际 %d", original.Type, decoded.Type)
	}
	if decoded.RelPath != original.RelPath {
		t.Errorf("RelPath: 期望 %s, 实际 %s", original.RelPath, decoded.RelPath)
	}
	if decoded.Hash != original.Hash {
		t.Errorf("Hash: 期望 %s, 实际 %s", original.Hash, decoded.Hash)
	}
	if decoded.Size != original.Size {
		t.Errorf("Size: 期望 %d, 实际 %d", original.Size, decoded.Size)
	}
	if decoded.ModTime != original.ModTime {
		t.Errorf("ModTime: 期望 %d, 实际 %d", original.ModTime, decoded.ModTime)
	}
	if decoded.PeerID != original.PeerID {
		t.Errorf("PeerID: 期望 %s, 实际 %s", original.PeerID, decoded.PeerID)
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp: 期望 %d, 实际 %d", original.Timestamp, decoded.Timestamp)
	}
}

func TestSyncMessageJSONOmitEmpty(t *testing.T) {
	// 空字段应被 omitempty 省略
	msg := SyncMessage{Type: MsgError}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	raw := string(data)
	if strings.Contains(raw, `"rel_path"`) {
		t.Errorf("空 RelPath 不应出现在 JSON 中: %s", raw)
	}
	if strings.Contains(raw, `"hash"`) {
		t.Errorf("空 Hash 不应出现在 JSON 中: %s", raw)
	}
	if strings.Contains(raw, `"peer_id"`) {
		t.Errorf("空 PeerID 不应出现在 JSON 中: %s", raw)
	}
	if strings.Contains(raw, `"timestamp"`) {
		t.Errorf("零 Timestamp 不应出现在 JSON 中: %s", raw)
	}
}

func TestSyncMessageString(t *testing.T) {
	msg := SyncMessage{
		Type:    MsgNotify,
		RelPath: "test.txt",
		Hash:    "deadbeef",
		Size:    42,
		ModTime: 1000000,
		PeerID:  "pid-1",
	}
	s := msg.String()
	for _, want := range []string{"test.txt", "deadbeef", "42", "1000000", "pid-1"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() 缺失字段值: %s (输出: %s)", want, s)
		}
	}
}

func TestHandshakeMessageFields(t *testing.T) {
	// 验证握手消息可以正确携带 PeerID 和 Timestamp
	msg := SyncMessage{
		Type:      MsgHandShake,
		PeerID:    "some-uuid",
		Timestamp: 1715690000000,
	}
	data, _ := json.Marshal(msg)

	var decoded SyncMessage
	json.Unmarshal(data, &decoded)

	if decoded.PeerID != "some-uuid" {
		t.Errorf("PeerID: 期望 some-uuid, 实际 %s", decoded.PeerID)
	}
	if decoded.Timestamp != 1715690000000 {
		t.Errorf("Timestamp: 期望 1715690000000, 实际 %d", decoded.Timestamp)
	}
}
