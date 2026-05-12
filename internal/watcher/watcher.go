package watch

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Event struct {
	Op   fsnotify.Op
	Path string
}

type Watcher struct {
	fsWatcher *fsnotify.Watcher
	root      string
	// 防止写入循环
	mu       sync.RWMutex
	ignoring map[string]time.Time
}

func isPathSafe(path string) bool {
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

// 递归目录，并添加到watcher
func (w *Watcher) watchDirRecursion(path string) error {
	err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			err = w.fsWatcher.Add(path)
			if err != nil {
				return err
			}
			log.Printf("正在监听目录: %v\n", path)
		}
		return nil
	})

	return err
}

func NewWatcher(root string) *Watcher {
	fw, _ := fsnotify.NewWatcher()
	return &Watcher{
		fsWatcher: fw,
		root:      root,
		ignoring:  make(map[string]time.Time),
	}
}

func (w *Watcher) WatcherStart() {
	if err := w.watchDirRecursion(w.root); err != nil {
		log.Printf("[Error]: WatcherStart: Scan Dir Failt: %v", w.root)
	}

	// 循环接收channel
	go func() {
		for {
			select {
			case event, ok := <-w.fsWatcher.Events:
				// Safe Check
				if !ok {
					return
				}

				relPath, err := filepath.Rel(w.root, event.Name)
				if err != nil && !isPathSafe(relPath) {
					continue
				}

				// 防环过滤
				w.mu.Lock()
				ignoreTime, exist := w.ignoring[event.Name]
				w.mu.Unlock()
				if exist && time.Since(ignoreTime) < time.Second {
					continue
				}

				// 触发"写入"事件
				if event.Has(fsnotify.Write) {
					log.Printf("文件已被修改, %s\n", event.Name)
					// TODO:触发同步
				}

				// 触发“创建”事件
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						w.fsWatcher.Add(event.Name)
					}
					log.Printf("文件已经创建, %s\n", event.Name)
					// 在子目录中进行递归
					w.watchDirRecursion(event.Name) // 如果mkdir -p创建子目录的话
				}
			case err, ok := <-w.fsWatcher.Errors:
				if !ok {
					return
				}
				log.Println("监听出错", err)
			}
		}
	}()

	// 定时5秒清理一次ignorePath
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			w.mu.Lock()
			for path, tick := range w.ignoring {
				if time.Since(tick) > 5*time.Second {
					delete(w.ignoring, path)
				}
			}
			w.mu.Unlock()
		}
	}()
}

// 在接收到信息后，添加到ignorepath中
func (w *Watcher) AddIgnorePath(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.ignoring[path] = time.Now()
}

func (w *Watcher) WatcherStop() {
	w.fsWatcher.Close()
}
