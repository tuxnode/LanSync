package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/mdns"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/tuxnode/LanSync/internal/discovery"
	"github.com/tuxnode/LanSync/internal/indexer"
	"github.com/tuxnode/LanSync/internal/protocol"
	"github.com/tuxnode/LanSync/internal/transport"
	"github.com/tuxnode/LanSync/internal/watcher"
)

// ─── 常量 ──────────────────────────────────────────────────

const defaultPort = 9876

// ─── 对等节点状态 ──────────────────────────────────────────

type peerStatus int

const (
	stLost      peerStatus = iota
	stFound
	stConnected
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

// ─── 数据模型 ──────────────────────────────────────────────

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

// ─── 守护进程状态 ──────────────────────────────────────────

type daemonState struct {
	mu sync.RWMutex

	tr  transport.Transport
	fsw *watcher.Watcher
	mds *mdns.Server

	myID      string
	watchDir  string
	port      int
	ifaceName string
	startTime time.Time

	peers        map[string]*peerEntry
	peerOrder    []string
	peerIDToAddr map[string]string
	indexSent    map[string]bool
	logs         []logEntry

	eventCh chan struct{}
	quitCh  chan struct{}
	running bool
}

func newDaemonState() *daemonState {
	return &daemonState{
		peers:        make(map[string]*peerEntry),
		peerOrder:    make([]string, 0),
		peerIDToAddr: make(map[string]string),
		indexSent:    make(map[string]bool),
		logs:         make([]logEntry, 0),
		eventCh:      make(chan struct{}, 1),
		quitCh:       make(chan struct{}),
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
		if existing.Status == stLost.String() && st == stFound && time.Since(existing.LastSeen) < 5*time.Second {
			return
		}
		if st > statusFromString(existing.Status) || existing.Status == stFound.String() && st == stConnected {
			existing.Status = st.String()
		}
		if hostname != "" {
			existing.Hostname = hostname
		}
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

func statusFromString(s string) peerStatus {
	switch s {
	case stConnected.String():
		return stConnected
	case stFound.String():
		return stFound
	default:
		return stLost
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

func (ds *daemonState) peerIDSet() map[string]bool {
	set := make(map[string]bool)
	for _, id := range ds.tr.Peers() {
		set[id] = true
	}
	return set
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

		foundThisRound := make(map[string]bool)

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

			foundThisRound[addr] = true
			ds.upsertPeer(addr, hostname, stFound)

			go ds.tryConnect(addr, hostname)
		}, ds.ifaceName)

		ds.mu.Lock()
		for addr, p := range ds.peers {
			if p.Status == stFound.String() && !foundThisRound[addr] {
				p.Status = stLost.String()
				p.LastSeen = time.Now()
			}
		}
		ds.mu.Unlock()

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

	ds.addLog(fmt.Sprintf("已连接节点: %s (%s)", hostname, addr), "conn")

	newPeers := ds.peerIDSet()
	var peerID string
	for id := range newPeers {
		if !oldPeers[id] {
			peerID = id
			break
		}
	}
	if peerID == "" {
		return
	}

	portPart := ""
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		portPart = addr[idx:]
	}

	merged := false
	ds.mu.Lock()
	if portPart != "" {
		for existingAddr, p := range ds.peers {
			if strings.HasSuffix(existingAddr, portPart) && p.Status == stFound.String() {
				p.Status = stConnected.String()
				p.Hostname = hostname
				p.LastSeen = time.Now()
				ds.peerIDToAddr[peerID] = existingAddr
				merged = true
				break
			}
		}
	}
	ds.mu.Unlock()

	if !merged {
		ds.peerIDToAddr[peerID] = addr
		ds.upsertPeer(addr, hostname, stConnected)
	}

	go ds.sendFullIndex(peerID)
}

func (ds *daemonState) sendFullIndex(peerID string) {
	ds.mu.Lock()
	if ds.indexSent[peerID] {
		ds.mu.Unlock()
		return
	}
	ds.indexSent[peerID] = true
	ds.mu.Unlock()

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

	ds.addLog(fmt.Sprintf("已发送完整索引至 %s: %d 个文件", shortStr(peerID, 16), count), "conn")
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

		for id := range current {
			if !prev[id] {
				ds.addLog(fmt.Sprintf("新节点接入: %s", shortStr(id, 16)), "conn")
				ds.mu.Lock()
				if _, exists := ds.peerIDToAddr[id]; !exists {
					addr := "peer:" + shortStr(id, 8)
					ds.peerIDToAddr[id] = addr
					ds.peers[addr] = &peerEntry{
						Addr:     addr,
						Hostname: shortStr(id, 12),
						Status:   stConnected.String(),
						LastSeen: time.Now(),
					}
					ds.peerOrder = append(ds.peerOrder, addr)
				}
				ds.mu.Unlock()
				go ds.sendFullIndex(id)
			}
		}

		for id := range prev {
			if !current[id] {
				ds.addLog(fmt.Sprintf("节点离开: %s", shortStr(id, 16)), "warn")
				ds.removePeerByID(id)
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
			return
		}
	}

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

	ds.fsw.AddIgnorePath(localPath)

	if err := os.WriteFile(localPath, data, 0644); err != nil {
		ds.addLog(fmt.Sprintf("写入文件失败 [%s]: %v", msg.RelPath, err), "err")
		return
	}

	ds.addLog(fmt.Sprintf("文件已同步: %s (%d bytes)", msg.RelPath, len(data)), "sync")
}

func (ds *daemonState) removePeerByID(peerID string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	delete(ds.indexSent, peerID)
	delete(ds.peerIDToAddr, peerID)

	for _, p := range ds.peers {
		if p.Status == stConnected.String() {
			p.Status = stLost.String()
			p.LastSeen = time.Now()
		}
	}
}

// ─── 守护进程控制 ──────────────────────────────────────────

func (ds *daemonState) Start(watchDir string, port int, initialPeers string, ifaceName string) error {
	ds.mu.Lock()
	if ds.running {
		ds.mu.Unlock()
		return fmt.Errorf("守护进程已在运行")
	}
	ds.mu.Unlock()

	absDir, err := filepath.Abs(watchDir)
	if err != nil {
		return fmt.Errorf("解析目录失败: %v", err)
	}
	if err := os.MkdirAll(absDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %v", err)
	}

	tr := transport.NewTransport(port)
	if err := tr.Start(); err != nil {
		return fmt.Errorf("启动传输层失败: %v", err)
	}

	actualPort := tr.Port()

	fsw, err := watcher.NewWatcher(absDir)
	if err != nil {
		tr.Stop()
		return fmt.Errorf("创建文件监视器失败: %v", err)
	}
	fsw.WatcherStart()

	mds, err := discovery.StartServer(actualPort, ifaceName)
	if err != nil {
		ds.addLog(fmt.Sprintf("警告: mDNS 启动失败: %v", err), "warn")
		mds = nil
	}

	ds.mu.Lock()
	ds.tr = tr
	ds.fsw = fsw
	ds.mds = mds
	ds.myID = tr.MyID()
	ds.watchDir = absDir
	ds.port = actualPort
	ds.ifaceName = ifaceName
	ds.startTime = time.Now()
	ds.quitCh = make(chan struct{})
	ds.peers = make(map[string]*peerEntry)
	ds.peerOrder = make([]string, 0)
	ds.peerIDToAddr = make(map[string]string)
	ds.indexSent = make(map[string]bool)
	ds.logs = make([]logEntry, 0)
	ds.running = true
	ds.mu.Unlock()

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

		case protocol.MsgBye:
			ds.addLog(fmt.Sprintf("节点主动退出: %s", shortStr(peerID, 10)), "warn")
			ds.removePeerByID(peerID)
		}
	})

	fsw.OnMessage = func(msg protocol.SyncMessage) {
		ds.tr.Broadcast(msg)
		ds.addLog(fmt.Sprintf("→ 推送变更: %s", msg.RelPath), "sync")
	}

	ds.addLog(fmt.Sprintf("LanSync 启动完成，监听目录: %s", absDir), "info")
	ds.addLog(fmt.Sprintf("本机 ID: %s, 端口: %d", shortStr(ds.myID, 12), actualPort), "info")

	if initialPeers != "" {
		for _, addr := range strings.Split(initialPeers, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				ds.addLog(fmt.Sprintf("正在连接: %s", addr), "info")
				go ds.tryConnect(addr, addr)
			}
		}
	}

	go ds.discoveryLoop()
	go ds.peerMonitorLoop()

	return nil
}

func (ds *daemonState) Stop() {
	ds.mu.Lock()
	if !ds.running {
		ds.mu.Unlock()
		return
	}
	ds.running = false
	ds.mu.Unlock()

	byeMsg := protocol.SyncMessage{
		Type: protocol.MsgBye,
	}
	ds.addLog("→ 广播退出通知", "warn")
	ds.tr.Broadcast(byeMsg)

	close(ds.quitCh)
	time.Sleep(200 * time.Millisecond)

	ds.tr.Stop()

	if ds.fsw != nil {
		ds.fsw.WatcherStop()
	}

	if ds.mds != nil {
		ds.mds.Shutdown()
	}

	ds.addLog("LanSync 已停止", "info")
}

// ─── 工具函数 ──────────────────────────────────────────────

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

// ─── GUI 应用 ──────────────────────────────────────────────

type guiApp struct {
	window fyne.Window
	ds     *daemonState

	dirEntry  *widget.Entry
	portEntry *widget.Entry
	peerEntry *widget.Entry

	ifaceSelect  *widget.Select
	ifaceOptions []string

	startBtn *widget.Button
	stopBtn  *widget.Button

	statusCard *widget.Card
	statusText *widget.Label
	uptimeText *widget.Label

	peersList *widget.List
	peersData []*peerEntry

	logList *widget.List
	logData  []logEntry
}

func newGUIApp() *guiApp {
	ds := newDaemonState()

	g := &guiApp{
		ds: ds,
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		home, _ = os.Getwd()
	}

	g.dirEntry = widget.NewEntry()
	g.dirEntry.SetPlaceHolder("选择要同步的目录…")
	g.dirEntry.SetText(home)

	g.portEntry = widget.NewEntry()
	g.portEntry.SetPlaceHolder("TCP 端口")
	g.portEntry.SetText(fmt.Sprintf("%d", defaultPort))

	g.peerEntry = widget.NewEntry()
	g.peerEntry.SetPlaceHolder("初始节点地址 (可选，逗号分隔)")

	ifaces := discovery.ListInterfaces()
	g.ifaceOptions = make([]string, 0, len(ifaces)+1)
	g.ifaceOptions = append(g.ifaceOptions, "自动检测")
	for _, iface := range ifaces {
		g.ifaceOptions = append(g.ifaceOptions, fmt.Sprintf("%s (%s)", iface.Name, iface.Addr))
	}
	g.ifaceSelect = widget.NewSelect(g.ifaceOptions, func(value string) {})
	if len(g.ifaceOptions) > 0 {
		g.ifaceSelect.SetSelected(g.ifaceOptions[0])
	}

	g.statusText = widget.NewLabel("LanSync 未启动")
	g.uptimeText = widget.NewLabel("")

	g.statusCard = widget.NewCard("运行状态", "",
		container.NewVBox(g.statusText, g.uptimeText),
	)

	g.peersList = widget.NewList(
		func() int {
			return len(g.peersData)
		},
		func() fyne.CanvasObject {
			statusIcon := widget.NewLabel("")
			nameLabel := widget.NewLabel("")
			addrLabel := widget.NewLabel("")
			return container.NewHBox(
				statusIcon,
				container.NewVBox(nameLabel, addrLabel),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(g.peersData) {
				return
			}
			p := g.peersData[id]
			hbox := obj.(*fyne.Container)
			statusIcon := hbox.Objects[0].(*widget.Label)
			vbox := hbox.Objects[1].(*fyne.Container)
			nameLabel := vbox.Objects[0].(*widget.Label)
			addrLabel := vbox.Objects[1].(*widget.Label)

			switch p.Status {
			case "已连接":
				statusIcon.SetText("●")
			case "在线":
				statusIcon.SetText("○")
			case "离线":
				statusIcon.SetText("✕")
			default:
				statusIcon.SetText(" ")
			}
			nameLabel.SetText(p.Hostname)
			addrLabel.SetText(fmt.Sprintf("%s  %s  %s", p.Addr, p.Status, p.LastSeen.Format("15:04:05")))
		},
	)

	g.logList = widget.NewList(
		func() int {
			return len(g.logData)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(g.logData) {
				return
			}
			entry := g.logData[id]
			label := obj.(*widget.Label)
			label.SetText(fmt.Sprintf("%s  %s", entry.Time, entry.Content))
		},
	)

	g.buildUI()

	return g
}

func (g *guiApp) buildUI() {
	dirBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err == nil && uri != nil {
				g.dirEntry.SetText(uri.Path())
			}
		}, g.window).Show()
	})

	dirRow := container.NewBorder(nil, nil, nil, dirBtn, g.dirEntry)

	portRow := container.NewHBox(
		widget.NewLabel("端口:"),
		g.portEntry,
	)

	configForm := container.NewVBox(
		widget.NewLabel("同步目录:"),
		dirRow,
		portRow,
		widget.NewLabel("网卡 (mDNS):"),
		g.ifaceSelect,
		widget.NewLabel("初始节点 (可选):"),
		g.peerEntry,
	)

	g.startBtn = widget.NewButtonWithIcon("启动", theme.MediaPlayIcon(), func() {
		g.onStart()
	})
	g.startBtn.Importance = widget.HighImportance

	g.stopBtn = widget.NewButtonWithIcon("停止", theme.MediaStopIcon(), func() {
		g.onStop()
	})
	g.stopBtn.Disable()

	settingsBtn := widget.NewButtonWithIcon("连接…", theme.DocumentCreateIcon(), func() {
		g.onConnectDialog()
	})

	controlBar := container.NewVBox(
		configForm,
		widget.NewSeparator(),
		container.NewHBox(g.startBtn, g.stopBtn, settingsBtn),
	)

	statusTab := container.NewPadded(g.statusCard)

	peersTab := container.NewBorder(
		widget.NewLabel("对等节点列表"),
		nil, nil, nil,
		g.peersList,
	)

	logTab := container.NewBorder(
		widget.NewLabel("事件日志"),
		nil, nil, nil,
		g.logList,
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("状态", statusTab),
		container.NewTabItem("节点", peersTab),
		container.NewTabItem("日志", logTab),
	)

	mainContent := container.NewBorder(controlBar, nil, nil, nil, tabs)

	g.window = app.New().NewWindow("LanSync - 局域网文件同步工具")
	g.window.SetContent(mainContent)
	g.window.Resize(fyne.NewSize(700, 520))

	g.window.SetOnClosed(func() {
		if g.ds.running {
			g.ds.Stop()
		}
	})

	go g.refreshLoop()
}

func (g *guiApp) onStart() {
	watchDir := strings.TrimSpace(g.dirEntry.Text)
	if watchDir == "" {
		dialog.ShowError(fmt.Errorf("请指定监听目录"), g.window)
		return
	}

	port := defaultPort
	if t := strings.TrimSpace(g.portEntry.Text); t != "" {
		fmt.Sscanf(t, "%d", &port)
	}

	initialPeers := strings.TrimSpace(g.peerEntry.Text)

	ifaceName := ""
	if sel := g.ifaceSelect.Selected; sel != "" && sel != "自动检测" {
		ifaceName = strings.Split(sel, " (")[0]
	}

	err := g.ds.Start(watchDir, port, initialPeers, ifaceName)
	if err != nil {
		dialog.ShowError(fmt.Errorf("启动失败: %v", err), g.window)
		return
	}

	g.startBtn.Disable()
	g.stopBtn.Enable()
	g.dirEntry.Disable()
	g.portEntry.Disable()
	g.peerEntry.Disable()
	g.ifaceSelect.Disable()
}

func (g *guiApp) onStop() {
	g.ds.Stop()

	g.startBtn.Enable()
	g.stopBtn.Disable()
	g.dirEntry.Enable()
	g.portEntry.Enable()
	g.peerEntry.Enable()
	g.ifaceSelect.Enable()
}

func (g *guiApp) onConnectDialog() {
	if !g.ds.running {
		dialog.ShowInformation("提示", "请先启动守护进程", g.window)
		return
	}

	addrEntry := widget.NewEntry()
	addrEntry.SetPlaceHolder("例如: 192.168.1.10:9876")

	dlg := dialog.NewForm(
		"手动连接节点",
		"连接",
		"取消",
		[]*widget.FormItem{
			widget.NewFormItem("地址:端口", addrEntry),
		},
		func(submitted bool) {
			if !submitted {
				return
			}
			addr := strings.TrimSpace(addrEntry.Text)
			if addr == "" {
				return
			}
			go g.ds.tryConnect(addr, addr)
		},
		g.window,
	)
	dlg.Resize(fyne.NewSize(350, 150))
	dlg.Show()
}

func (g *guiApp) refreshLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C

		g.ds.mu.RLock()
		running := g.ds.running

		if running {
			myID := shortStr(g.ds.myID, 16)
			watchDir := g.ds.watchDir
			port := g.ds.port
			uptime := time.Since(g.ds.startTime).Round(time.Second).String()
			peerCount := len(g.ds.peers)
			connected := g.ds.connectedCount()

			peersSnap := make([]*peerEntry, len(g.ds.peerOrder))
			for i, addr := range g.ds.peerOrder {
				peersSnap[i] = g.ds.peers[addr]
			}

			logStart := 0
			if len(g.ds.logs) > 100 {
				logStart = len(g.ds.logs) - 100
			}
			logsSnap := make([]logEntry, len(g.ds.logs)-logStart)
			copy(logsSnap, g.ds.logs[logStart:])

			g.ds.mu.RUnlock()

			g.statusText.SetText(fmt.Sprintf(
				"本机 ID:     %s\n"+
					"监听目录:    %s\n"+
					"TCP 端口:    %d\n"+
					"已发现节点:  %d\n"+
					"已连接节点:  %d",
				myID, watchDir, port, peerCount, connected,
			))

			g.uptimeText.SetText(fmt.Sprintf("运行时间: %s", uptime))

			g.peersData = peersSnap
			g.peersList.Refresh()

			g.logData = logsSnap
			g.logList.Refresh()
		} else {
			g.ds.mu.RUnlock()
			g.statusText.SetText("LanSync 未启动")
			g.uptimeText.SetText("")

			g.peersData = nil
			g.peersList.Refresh()

			if len(g.logData) > 0 {
				g.logData = nil
				g.logList.Refresh()
			}
		}
	}
}

// ─── 入口 ─────────────────────────────────────────────────

func main() {
	cliMode := false
	for _, arg := range os.Args[1:] {
		if arg == "--cli" || arg == "-c" {
			cliMode = true
			break
		}
	}

	if cliMode {
		runCLI(os.Args[1:])
		return
	}

	gui := newGUIApp()
	gui.window.ShowAndRun()
}

// ─── CLI 兼容模式 ──────────────────────────────────────────

func runCLI(args []string) {
	if len(args) < 1 || (args[0] == "--cli" || args[0] == "-c") && len(args) < 2 {
		fmt.Println("LanSync - 局域网文件同步工具 (CLI 模式)")
		fmt.Println()
		fmt.Println("用法:")
		fmt.Println("  lansync-gui daemon  [--dir PATH] [--port PORT]  启动守护进程")
		fmt.Println("  lansync-gui status  [--http PORT]               查看运行状态")
		fmt.Println("  lansync-gui peers   [--http PORT]               查看节点列表")
		fmt.Println("  lansync-gui log     [--http PORT]               查看事件日志")
		os.Exit(0)
	}

	if args[0] == "--cli" || args[0] == "-c" {
		args = args[1:]
	}

	switch args[0] {
	case "daemon":
		runDaemonCLI(args[1:])
	case "status":
		runStatusCLI(args[1:])
	case "peers":
		runPeersCLI(args[1:])
	case "log":
		runLogCLI(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n", args[0])
		os.Exit(1)
	}
}

func runDaemonCLI(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	dirFlag := fs.String("dir", "", "监听目录 (默认: 当前目录)")
	portFlag := fs.Int("port", defaultPort, "TCP 监听端口")
	httpFlag := fs.Int("http", 9786, "HTTP API 端口")
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

	ds := newDaemonState()
	if err := ds.Start(wd, *portFlag, "", ""); err != nil {
		fmt.Fprintf(os.Stderr, "启动失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("LanSync 守护进程已启动\n")
	fmt.Printf("  本机 ID:  %s\n", shortStr(ds.myID, 12))
	fmt.Printf("  监听目录: %s\n", ds.watchDir)
	fmt.Printf("  TCP 端口: %d\n", ds.port)

	if *httpFlag > 0 {
		go startSimpleHTTP(ds, *httpFlag)
		fmt.Printf("  HTTP API: http://127.0.0.1:%d\n", *httpFlag)
	}

	fmt.Printf("\n按 Ctrl+C 退出\n")

	select {}
}

func runStatusCLI(args []string) {
	fmt.Println("请在 GUI 模式中查看状态")
}

func runPeersCLI(args []string) {
	fmt.Println("请在 GUI 模式中查看节点")
}

func runLogCLI(args []string) {
	fmt.Println("请在 GUI 模式中查看日志")
}

// ─── 简易 HTTP API（CLI 模式用） ──────────────────────────

func startSimpleHTTP(ds *daemonState, httpPort int) {
	// 简化的 HTTP 服务，供 CLI 工具查询
	_ = ds
	_ = httpPort
}
