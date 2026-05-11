package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateIndex(t *testing.T) {
	// 1. 创建临时测试目录
	tmpDir, _ := os.MkdirTemp("", "indexer_test")
	defer os.RemoveAll(tmpDir)

	// 2. 创建一个测试文件
	testFile := filepath.Join(tmpDir, "hello.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	// 3. 生成索引
	index, err := GeneralIndex(tmpDir)
	if err != nil {
		t.Fatalf("Failed to generate index: %v", err)
	}

	// 4. 验证结果
	if info, ok := index["hello.txt"]; ok {
		t.Logf("Found file: %s, Hash: %s", info.RelPath, info.Hash)
		expectedHash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
		if info.Hash != expectedHash {
			t.Errorf("Hash mismatch! Expected %s, got %s", expectedHash, info.Hash)
		}
	} else {
		t.Error("Test file not found in index")
	}
}
