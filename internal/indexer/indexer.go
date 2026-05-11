package indexer

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// 扫描目录并生成index
func GeneralIndex(root string) (IndexMap, error) {
	index := make(IndexMap)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		// 返回相对路径
		relPath, _ := filepath.Rel(root, path)
		if relPath == "." {
			return nil
		}

		// 防止路径穿越
		if relPath == "../" {
			return nil
		}

		// 获取目录信息
		info, _ := d.Info()
		fileinfo := FileInfo{
			RelPath:  relPath,
			Size:     info.Size(),
			ModTime:  info.ModTime().Unix(),
			IsFolder: d.IsDir(),
		}

		// 如果是文件，则处理hash
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
