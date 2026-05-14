package watcher

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/tuxnode/LanSync/internal/indexer"
	"github.com/tuxnode/LanSync/internal/protocol"
)

type Event struct {
	Op   fsnotify.Op
	Path string
}

type Watcher struct {
	fsWatcher *fsnotify.Watcher
	root      string
	mu        sync.RWMutex
	ignoring  map[string]time.Time
	OnMessage func(msg protocol.SyncMessage)
	done      chan struct{}
}

func (w *Watcher) watchDirRecursion(path string) error {
	return filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if err := w.fsWatcher.Add(path); err != nil {
				return err
			}
			log.Printf("正在监听目录: %v", path)
		}
		return nil
	})
}

func NewWatcher(root string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &Watcher{
		fsWatcher: fw,
		root:      root,
		ignoring:  make(map[string]time.Time),
		done:      make(chan struct{}),
	}, nil
}

func (w *Watcher) handleFileChange(absPath string) {
	relPath, err := filepath.Rel(w.root, absPath)
	if err != nil {
		return
	}
	relPath = filepath.ToSlash(relPath)

	info, err := os.Stat(absPath)
	if err != nil {
		return
	}

	fileHash, err := indexer.CaculateHash(absPath)
	if err != nil {
		return
	}

	msg := protocol.SyncMessage{
		Type:    protocol.MsgNotify,
		RelPath: relPath,
		Hash:    fileHash,
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
	}

	if w.OnMessage != nil {
		w.OnMessage(msg)
	}
}

func (w *Watcher) AddIgnorePath(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ignoring[path] = time.Now()
}

func (w *Watcher) WatcherStop() {
	close(w.done)
	w.fsWatcher.Close()
}

func (w *Watcher) WatcherStart() {
	if err := w.watchDirRecursion(w.root); err != nil {
		log.Printf("[Error] WatcherStart: Scan Dir Failed: %v", err)
	}

	go func() {
		for {
			select {
			case event, ok := <-w.fsWatcher.Events:
				if !ok {
					return
				}

				relPath, err := filepath.Rel(w.root, event.Name)
				if err != nil || !indexer.IsPathSafe(relPath) {
					continue
				}

				w.mu.Lock()
				ignoreTime, exist := w.ignoring[event.Name]
				w.mu.Unlock()
				if exist && time.Since(ignoreTime) < time.Second {
					continue
				}

				if event.Has(fsnotify.Write) {
					log.Printf("文件已被修改: %s", event.Name)
					w.handleFileChange(event.Name)
				}

			if event.Has(fsnotify.Create) {
				info, err := os.Stat(event.Name)
				if err == nil && info.IsDir() {
					w.fsWatcher.Add(event.Name)
					log.Printf("正在监听目录: %v", event.Name)
					w.watchDirRecursion(event.Name)
				}
				log.Printf("文件已经创建: %s", event.Name)
				if err == nil && !info.IsDir() {
					w.handleFileChange(event.Name)
				}
			}
			case err, ok := <-w.fsWatcher.Errors:
				if !ok {
					return
				}
				log.Println("监听出错:", err)
			case <-w.done:
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				w.mu.Lock()
				for path, tick := range w.ignoring {
					if time.Since(tick) > 5*time.Second {
						delete(w.ignoring, path)
					}
				}
				w.mu.Unlock()
			case <-w.done:
				return
			}
		}
	}()
}
