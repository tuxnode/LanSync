package indexer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestPathTraversal(t *testing.T) {
	tests := []struct {
		path   string
		isSafe bool
	}{
		{"hello.txt", true},
		{"docs/images/1.png", true},
		{"../hello.txt", false},            // 向上跳转
		{"/etc/passwd", false},             // 绝对路径
		{"subdir/../../etc/shadow", false}, // 混淆跳转
		{"..\\windows\\system32", false},   // Windows 风格跳转
	}
	for _, tt := range tests {
		if IsPathSafe(tt.path) != tt.isSafe {
			t.Errorf("Security Check Failed for path: %v. Expected safe=%v", tt.path, tt.isSafe)
		}
	}
}

func IsPathSafe(path string) bool {
	cleanPath := filepath.ToSlash(path)

	if filepath.IsAbs(path) || strings.HasPrefix(cleanPath, "/") {
		return false
	}

	// 检查是否有跳转符号
	parts := strings.Split(path, string("/"))
	if slices.Contains(parts, "..") {
		return false
	}
	return true
}
