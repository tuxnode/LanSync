package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// IsPathSafe 检查相对路径是否安全（无路径穿越、非绝对路径）。
func IsPathSafe(relPath string) bool {
	if filepath.IsAbs(relPath) {
		return false
	}

	// 统一斜杠方向，处理 Windows 风格路径
	normalized := strings.ReplaceAll(relPath, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return false
	}

	parts := strings.Split(normalized, "/")
	return !slices.Contains(parts, "..")
}

// GeneralIndex 扫描目录并生成 FileInfo 索引。
func GeneralIndex(root string) (IndexMap, error) {
	index := make(IndexMap)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if relPath == "." {
			return nil
		}

		if !IsPathSafe(relPath) {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}

		fileinfo := FileInfo{
			RelPath:  relPath,
			Size:     info.Size(),
			ModTime:  info.ModTime().Unix(),
			IsFolder: d.IsDir(),
		}

		if !d.IsDir() {
			hash, err := CaculateHash(path)
			if err != nil {
				return err
			}
			fileinfo.Hash = hash
		}
		index[relPath] = fileinfo

		return nil
	})

	return index, err
}

// 计算文件hash
func CaculateHash(filepath string) (string, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
