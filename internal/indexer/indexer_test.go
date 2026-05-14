package indexer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateIndex(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	index, err := GeneralIndex(tmpDir)
	if err != nil {
		t.Fatalf("生成索引失败: %v", err)
	}

	if info, ok := index["hello.txt"]; ok {
		expectedHash := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
		if info.Hash != expectedHash {
			t.Errorf("Hash: 期望 %s, 实际 %s", expectedHash, info.Hash)
		}
		if info.Size != 11 {
			t.Errorf("Size: 期望 11, 实际 %d", info.Size)
		}
		if info.IsFolder {
			t.Error("文件被误判为目录")
		}
	} else {
		t.Error("索引中未找到 hello.txt")
	}
}

func TestGenerateIndexWithDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(subDir, "nested.txt")
	if err := os.WriteFile(nestedFile, []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}

	index, err := GeneralIndex(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// 目录也被索引
	if info, ok := index["subdir"]; !ok || !info.IsFolder {
		t.Error("目录 subdir 未被正确索引")
	}

	// 嵌套文件也被索引
	if info, ok := index["subdir/nested.txt"]; !ok {
		t.Error("嵌套文件未被索引")
	} else if info.Hash == "" {
		t.Error("嵌套文件 Hash 为空")
	}
}

func TestGenerateIndexEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	index, err := GeneralIndex(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 0 {
		t.Errorf("空目录索引应为空，实际 %d 条", len(index))
	}
}

func TestIsPathSafe(t *testing.T) {
	tests := []struct {
		path   string
		isSafe bool
	}{
		{"hello.txt", true},
		{"docs/images/1.png", true},
		{"../hello.txt", false},
		{"/etc/passwd", false},
		{"subdir/../../etc/shadow", false},
		{"..\\windows\\system32", false},
		{".", true},
		{"", true},
		{"a/b/..", false},
	}
	for _, tt := range tests {
		if IsPathSafe(tt.path) != tt.isSafe {
			t.Errorf("IsPathSafe(%q) = %v, 期望 %v", tt.path, IsPathSafe(tt.path), tt.isSafe)
		}
	}
}

func TestCalculateHash(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "hash_test.txt")
	content := "hello world"
	os.WriteFile(testFile, []byte(content), 0644)

	hash, err := CaculateHash(testFile)
	if err != nil {
		t.Fatal(err)
	}

	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("Hash: 期望 %s, 实际 %s", expected, hash)
	}

	// 不存在的文件
	_, err = CaculateHash(filepath.Join(tmpDir, "nonexistent"))
	if err == nil {
		t.Error("不存在的文件应返回错误")
	}
}
