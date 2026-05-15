package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/mdns"

	"github.com/tuxnode/LanSync/internal/discovery"
	"github.com/tuxnode/LanSync/internal/indexer"
	"github.com/tuxnode/LanSync/internal/protocol"
	"github.com/tuxnode/LanSync/internal/transport"
	"github.com/tuxnode/LanSync/internal/watcher"
)

// ─── 共享状态 ──────────────────────────────────────────────

type peerStatus int

const (
	stFound     peerStatus = iota
	stConnected
	stLost
)

func (s peerStatus) String() string {
	switch s {
	case stFound:
		return "在线"
	case stConnected:
		return "已连接"
	case stLost:
		return "离线"
	}
	return "未知"
}

type peerEntry struct {
	Addr     string    `json:"addr"`
	Hostname string    `json:"hostname"`
	Status   string    `json:"status"`
	LastSeen time.Time `json:"last_seen"`
}

type logEntry struct {
	Time    string `json:"time"`
	Content string `json:"content"`
	Level   string `json:"level"`
}

type daemonState struct {
	mu sync.RWMutex

	tr  transport.Transport
	fsw *watcher.Watcher
	mds *mdns.Server

	myID      string
	watchDir  string
	port      int
	startTime time.Time

	peers     map[string]*peerEntry
	peerOrder []string
	logs      []logEntry

	eventCh chan struct{}
	quitCh  chan struct{}
}

func newDaemonState() *daemonState {
	return &daemonState{
		peers:     make(map[string]*peerEntry),
		peerOrder: make([]string, 0),
		logs:      make([]logEntry, 0),
		eventCh:   make(chan struct{}, 1),
		quitCh:    make(chan struct{}),
	}
}

func (ds *daemonState) addLog(content, level string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.logs = append(ds.logs, logEntry{
		Time:    time.Now().Format("15:04:05"),
		Content: content,
		Level:   level,
	})
	if len(ds.logs) > 500 {
		ds.logs = ds.logs[len(ds.logs)-500:]
	}
}

func (ds *daemonState) upsertPeer(addr, hostname string, st peerStatus) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if existing, ok := ds.peers[addr]; ok {
		existing.Status = st.String()
		existing.Hostname = hostname
		existing.LastSeen = time.Now()
	} else {
		ds.peers[addr] = &peerEntry{
			Addr:     addr,
			Hostname: hostname,
			Status:   st.String(),
			LastSeen: time.Now(),
		}
		ds.peerOrder = append(ds.peerOrder, addr)
	}
}

func (ds *daemonState) connectedCount() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	n := 0
	for _, p := range ds.peers {
		if p.Status == stConnected.String() {
			n++
		}
	}
	return n
}

// ─── HTTP API ──────────────────────────────────────────────

func (ds *daemonState) serveHTTP(httpPort int) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		ds.mu.RLock()
		defer ds.mu.RUnlock()

		writeJSON(w, map[string]interface{}{
			"my_id":      ds.myID,
			"watch_dir":  ds.watchDir,
			"port":       ds.port,
			"http_port":  httpPort,
			"peer_count": len(ds.peers),
			"connected":  ds.connectedCount(),
			"uptime":     time.Since(ds.startTime).Round(time.Second).String(),
			"start_time": ds.startTime.Format(time.RFC3339),
		})
	})

	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		ds.mu.RLock()
		defer ds.mu.RUnlock()

		peers := make([]*peerEntry, 0, len(ds.peerOrder))
		for _, addr := range ds.peerOrder {
			peers = append(peers, ds.peers[addr])
		}
		writeJSON(w, map[string]interface{}{"peers": peers})
	})

	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		ds.mu.RLock()
		defer ds.mu.RUnlock()

		start := 0
		if len(ds.logs) > 50 {
			start = len(ds.logs) - 50
		}
		writeJSON(w, map[string]interface{}{"logs": ds.logs[start:]})
	})

	mux.HandleFunc("/api/index", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		idx, err := indexer.GeneralIndex(ds.watchDir)
		if err != nil {
			writeJSON(w, map[string]interface{}{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]interface{}{
			"count":   len(idx),
			"elapsed": time.Since(start).Round(time.Millisecond).String(),
		})
	})

	mux.HandleFunc("/api/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]interface{}{"error": "仅支持 POST"})
			return
		}

		var body struct {
			Addr string `json:"addr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Addr == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]interface{}{"error": "缺少 addr 参数"})
			return
		}

		go ds.tryConnect(body.Addr, body.Addr)

		writeJSON(w, map[string]interface{}{
			"status": "ok",
			"addr":   body.Addr,
		})
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", httpPort),
		Handler: mux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "HTTP 服务异常: %v\n", err)
		}
	}()

	return srv
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

// ─── 后台协程 ──────────────────────────────────────────────

func (ds *daemonState) discoveryLoop() {
	defer func() {
		if r := recover(); r != nil {
			ds.addLog(fmt.Sprintf("发现服务异常: %v", r), "err")
		}
	}()

	for {
		select {
		case <-ds.quitCh:
			return
		default:
		}

		discovery.DiscoverNodes(func(entry *mdns.ServiceEntry) {
			if entry.AddrV4 == nil || len(entry.AddrV4) == 0 {
				return
			}
			addr := fmt.Sprintf("%s:%d", entry.AddrV4, entry.Port)
			if strings.HasPrefix(addr, "127.") ||
				strings.HasPrefix(addr, "localhost") {
				return
			}

			hostname := entry.Name
			if idx := strings.Index(hostname, "."); idx > 0 {
				hostname = hostname[:idx]
			}
			hostname = strings.ReplaceAll(hostname, "-", ".")

			ds.upsertPeer(addr, hostname, stFound)

			go ds.tryConnect(addr, hostname)
		})

		select {
		case <-ds.quitCh:
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func (ds *daemonState) tryConnect(addr, hostname string) {
	oldPeers := ds.peerIDSet()

	if err := ds.tr.ConnectTo(addr); err != nil {
		return
	}

	ds.upsertPeer(addr, hostname, stConnected)
	ds.addLog(fmt.Sprintf("已连接节点: %s (%s)", hostname, addr), "conn")

	// 找到新连接节点的 PeerID
	newPeers := ds.peerIDSet()
	for id := range newPeers {
		if !oldPeers[id] {
			go ds.sendFullIndex(id)
			break
		}
	}
}

func (ds *daemonState) peerIDSet() map[string]bool {
	set := make(map[string]bool)
	for _, id := range ds.tr.Peers() {
		set[id] = true
	}
	return set
}

func (ds *daemonState) sendFullIndex(peerID string) {
	idx, err := indexer.GeneralIndex(ds.watchDir)
	if err != nil {
		ds.addLog(fmt.Sprintf("生成索引失败: %v", err), "err")
		return
	}

	count := 0
	for relPath, info := range idx {
		if info.IsFolder {
			continue
		}
		msg := protocol.SyncMessage{
			Type:    protocol.MsgNotify,
			RelPath: relPath,
			Hash:    info.Hash,
			Size:    info.Size,
			ModTime: info.ModTime,
		}
		if err := ds.tr.SendTo(peerID, msg); err != nil {
			ds.addLog(fmt.Sprintf("发送索引中断 [%s]: %v", relPath, err), "err")
			return
		}
		count++
	}

	ds.addLog(fmt.Sprintf("已发送完整索引至 %s: %d 个文件", shortStr(peerID, 12), count), "conn")
}

func (ds *daemonState) peerMonitorLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	prev := make(map[string]bool)

	for {
		select {
		case <-ds.quitCh:
			return
		case <-ticker.C:
		}

		current := make(map[string]bool)
		for _, id := range ds.tr.Peers() {
			current[id] = true
		}

		// 检测新连接的节点（入站连接）
		for id := range current {
			if !prev[id] && len(prev) > 0 {
				ds.addLog(fmt.Sprintf("新节点接入: %s", shortStr(id, 12)), "conn")
				go ds.sendFullIndex(id)
			}
		}

		// 检测离开的节点
		for id := range prev {
			if !current[id] {
				ds.addLog(fmt.Sprintf("节点离开: %s", shortStr(id, 12)), "warn")
			}
		}
		prev = current
	}
}

// ─── 同步处理 ──────────────────────────────────────────────

func (ds *daemonState) handleRecvNotify(peerID string, msg protocol.SyncMessage) {
	localPath := filepath.Join(ds.watchDir, msg.RelPath)

	info, err := os.Stat(localPath)
	if err == nil && !info.IsDir() {
		localHash, hashErr := indexer.CaculateHash(localPath)
		if hashErr == nil && localHash == msg.Hash {
			return // 文件已存在且 hash 一致，跳过
		}
	}

	// 文件缺失或 hash 不一致 → 请求下载
	req := protocol.SyncMessage{
		Type:    protocol.MsgPullRequest,
		RelPath: msg.RelPath,
	}
	if err := ds.tr.SendTo(peerID, req); err != nil {
		ds.addLog(fmt.Sprintf("请求下载失败 [%s]: %v", msg.RelPath, err), "err")
		return
	}
	ds.addLog(fmt.Sprintf("→ 请求下载: %s", msg.RelPath), "sync")
}

func (ds *daemonState) handleRecvPullRequest(peerID string, msg protocol.SyncMessage) {
	localPath := filepath.Join(ds.watchDir, msg.RelPath)

	data, err := os.ReadFile(localPath)
	if err != nil {
		errMsg := protocol.SyncMessage{
			Type:    protocol.MsgError,
			RelPath: msg.RelPath,
			Data:    fmt.Sprintf("读取文件失败: %v", err),
		}
		ds.tr.SendTo(peerID, errMsg)
		return
	}

	reply := protocol.SyncMessage{
		Type:    protocol.MsgFileData,
		RelPath: msg.RelPath,
		Size:    int64(len(data)),
		Data:    base64Encode(data),
	}
	if err := ds.tr.SendTo(peerID, reply); err != nil {
		ds.addLog(fmt.Sprintf("发送文件失败 [%s]: %v", msg.RelPath, err), "err")
		return
	}
	ds.addLog(fmt.Sprintf("→ 发送文件: %s (%d bytes)", msg.RelPath, len(data)), "sync")
}

func (ds *daemonState) handleRecvFileData(msg protocol.SyncMessage) {
	localPath := filepath.Join(ds.watchDir, msg.RelPath)

	data, err := base64Decode(msg.Data)
	if err != nil {
		ds.addLog(fmt.Sprintf("解码文件失败 [%s]: %v", msg.RelPath, err), "err")
		return
	}

	dir := filepath.Dir(localPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		ds.addLog(fmt.Sprintf("创建目录失败 [%s]: %v", dir, err), "err")
		return
	}

	// 写入前标记忽略，防止 watcher 检测到后回环广播
	ds.fsw.AddIgnorePath(localPath)

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		ds.addLog(fmt.Sprintf("写入文件失败 [%s]: %v", msg.RelPath, err), "err")
		return
	}

	ds.addLog(fmt.Sprintf("✓ 文件已同步: %s (%d bytes)", msg.RelPath, len(data)), "sync")
}

// ─── 守护进程 ──────────────────────────────────────────────

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	dirFlag := fs.String("dir", "", "监听目录 (默认: 当前目录)")
	portFlag := fs.Int("port", 9876, "TCP 监听端口")
	httpFlag := fs.Int("http", 9786, "HTTP API 端口")
	peerFlag := fs.String("peer", "", "初始连接地址，多个用逗号分隔 (例: 192.168.1.10:9876,192.168.1.11:9876)")
	fs.Parse(args)

	wd := *dirFlag
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "获取当前目录失败: %v\n", err)
			os.Exit(1)
		}
	}
	wd, _ = filepath.Abs(wd)

	// 确保监听目录存在
	if err := os.MkdirAll(wd, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "创建监听目录失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化传输层
	tr := transport.NewTransport(*portFlag)
	if err := tr.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "启动传输层失败: %v\n", err)
		os.Exit(1)
	}
	defer tr.Stop()

	actualPort := tr.Port()

	// 初始化文件监视器
	fsw, err := watcher.NewWatcher(wd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建文件监视器失败: %v\n", err)
		os.Exit(1)
	}
	fsw.WatcherStart()
	defer fsw.WatcherStop()

	// 初始化 mDNS
	mds, err := discovery.StartServer(actualPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动 mDNS 发现服务失败: %v\n", err)
		os.Exit(1)
	}
	defer mds.Shutdown()

	// 构建状态
	ds := newDaemonState()
	ds.tr = tr
	ds.fsw = fsw
	ds.mds = mds
	ds.myID = tr.MyID()
	ds.watchDir = wd
	ds.port = actualPort
	ds.startTime = time.Now()

	// 注册回调
	tr.OnMessage(func(peerID string, msg protocol.SyncMessage) {
		switch msg.Type {
		case protocol.MsgNotify:
			ds.addLog(fmt.Sprintf("← %s 收到变更: %s", shortStr(peerID, 10), msg.RelPath), "sync")
			go ds.handleRecvNotify(peerID, msg)

		case protocol.MsgPullRequest:
			ds.addLog(fmt.Sprintf("← %s 请求下载: %s", shortStr(peerID, 10), msg.RelPath), "info")
			go ds.handleRecvPullRequest(peerID, msg)

		case protocol.MsgFileData:
			ds.addLog(fmt.Sprintf("← %s 接收文件: %s (%d bytes)", shortStr(peerID, 10), msg.RelPath, len(msg.Data)), "sync")
			go ds.handleRecvFileData(msg)

		case protocol.MsgError:
			ds.addLog(fmt.Sprintf("← %s 错误: %s", shortStr(peerID, 10), msg.RelPath), "err")
		}
	})

	fsw.OnMessage = func(msg protocol.SyncMessage) {
		ds.tr.Broadcast(msg)
		ds.addLog(fmt.Sprintf("→ 推送变更: %s", msg.RelPath), "sync")
	}

	ds.addLog(fmt.Sprintf("LanSync 启动完成，监听目录: %s", wd), "info")
	ds.addLog(fmt.Sprintf("本机 ID: %s, 端口: %d", shortStr(ds.myID, 12), actualPort), "info")

	// 启动 HTTP API
	srv := ds.serveHTTP(*httpFlag)
	defer srv.Close()

	fmt.Printf("LanSync 守护进程已启动\n")
	fmt.Printf("  本机 ID:  %s\n", shortStr(ds.myID, 12))
	fmt.Printf("  监听目录: %s\n", wd)
	fmt.Printf("  TCP 端口: %d\n", actualPort)
	fmt.Printf("  HTTP API: http://127.0.0.1:%d\n", *httpFlag)
	fmt.Printf("\n使用 lansync status / peers / log 查看运行状态\n")

	// 连接 --peer 指定的初始节点
	if *peerFlag != "" {
		for _, addr := range strings.Split(*peerFlag, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				fmt.Printf("  正在连接: %s\n", addr)
				go ds.tryConnect(addr, addr)
			}
		}
	}

	// 启动后台协程
	go ds.discoveryLoop()
	go ds.peerMonitorLoop()

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\n正在关闭…")
	close(ds.quitCh)
	time.Sleep(200 * time.Millisecond)
}

// ─── CLI 查询命令 ──────────────────────────────────────────

func apiURL(httpPort int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", httpPort, path)
}

func getJSON(url string, v interface{}) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("无法连接守护进程 (%s)，请先运行 lansync daemon", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(body, v)
}

func mustGetJSON(url string) map[string]interface{} {
	var result map[string]interface{}
	if err := getJSON(url, &result); err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
	return result
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	httpPort := fs.Int("http", 9786, "HTTP API 端口")
	fs.Parse(args)

	data := mustGetJSON(apiURL(*httpPort, "/api/status"))

	fmt.Printf("本机 ID:    %s\n", data["my_id"])
	fmt.Printf("监听目录:   %s\n", data["watch_dir"])
	fmt.Printf("TCP 端口:   %.0f\n", data["port"])
	fmt.Printf("HTTP API:   http://127.0.0.1:%.0f\n", data["http_port"])
	fmt.Printf("已发现节点: %.0f\n", data["peer_count"])
	fmt.Printf("已连接节点: %.0f\n", data["connected"])
	fmt.Printf("运行时间:   %s\n", data["uptime"])
	fmt.Printf("启动时间:   %s\n", data["start_time"])
}

func runPeers(args []string) {
	fs := flag.NewFlagSet("peers", flag.ExitOnError)
	httpPort := fs.Int("http", 9786, "HTTP API 端口")
	fs.Parse(args)

	data := mustGetJSON(apiURL(*httpPort, "/api/peers"))
	peers, _ := data["peers"].([]interface{})

	if len(peers) == 0 {
		fmt.Println("暂无已发现的节点")
		return
	}

	fmt.Printf("%-25s %-22s %s\n", "地址", "主机名", "状态")
	fmt.Println(strings.Repeat("-", 60))
	for _, p := range peers {
		peer := p.(map[string]interface{})
		status := peer["status"].(string)
		marker := " "
		switch status {
		case "已连接":
			marker = "●"
		case "在线":
			marker = "○"
		case "离线":
			marker = "✕"
		}
		fmt.Printf("%s %-23s %-22s %s\n", marker, peer["addr"], peer["hostname"], status)
	}
}

func runLog(args []string) {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	httpPort := fs.Int("http", 9786, "HTTP API 端口")
	fs.Parse(args)

	data := mustGetJSON(apiURL(*httpPort, "/api/log"))
	logs, _ := data["logs"].([]interface{})

	for _, l := range logs {
		entry := l.(map[string]interface{})
		fmt.Printf("%s %s\n", entry["time"], entry["content"])
	}
}

func runConnect(args []string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "用法: lansync connect <地址:端口> [--http PORT]\n")
		fmt.Fprintf(os.Stderr, "示例: lansync connect 192.168.1.10:9876\n")
		os.Exit(1)
	}

	addr := args[0]
	httpPort := 9786
	for i := 1; i < len(args); i++ {
		if args[i] == "--http" && i+1 < len(args) {
			fmt.Sscanf(args[i+1], "%d", &httpPort)
			break
		}
	}

	body := fmt.Sprintf(`{"addr":"%s"}`, addr)
	resp, err := http.Post(
		apiURL(httpPort, "/api/connect"),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法连接守护进程: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	fmt.Println(string(data))
}

// ─── 入口 ─────────────────────────────────────────────────

func shortStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("LanSync - 局域网文件同步工具")
		fmt.Println()
		fmt.Println("用法:")
		fmt.Println("  lansync daemon  [--dir PATH] [--port PORT] [--peer ADDR]  启动守护进程")
		fmt.Println("  lansync connect <地址:端口>                                  手动连接节点")
		fmt.Println("  lansync status  [--http PORT]                               查看运行状态")
		fmt.Println("  lansync peers   [--http PORT]                               查看节点列表")
		fmt.Println("  lansync log     [--http PORT]                               查看事件日志")
		os.Exit(0)
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon(os.Args[2:])
	case "connect":
		runConnect(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "peers":
		runPeers(os.Args[2:])
	case "log":
		runLog(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", os.Args[1])
		os.Exit(1)
	}
}
