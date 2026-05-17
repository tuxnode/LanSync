# LanSync 
A Simple Golang LAN file synchronization program

Use Http API Background

## Usage

```bash

LanSync - 局域网文件同步工具

用法:
  lansync daemon  [--dir PATH] [--port PORT] [--peer ADDR]  启动守护进程
  lansync connect <地址:端口>                                  手动连接节点
  lansync status  [--http PORT]                               查看运行状态
  lansync peers   [--http PORT]                               查看节点列表
  lansync log     [--http PORT]                               查看事件日志

```

### Build

```bash
git clone https://github.com/tuxnode/LanSync.git
cd LanSync && go build ./cmd/...
```

跨平台编译：

```bash
# macOS / Linux
go build -o lansync ./cmd/lansync/

# Windows (在 macOS/Linux 上交叉编译)
GOOS=windows GOARCH=amd64 go build -o lansync.exe ./cmd/lansync/
```

编译GUI程序:
```bash
sudo apt install libxxf86vm-dev libxrandr-dev libxi-dev libxcursor-dev libxinerama-dev

go build ./cmd/lansync-gui/...

```

### Qt Desktop Client

A Qt Widgets desktop client is available under `clients/qt-lansync`.

It uses the same JSON `SyncMessage` protocol as the Go daemon and supports TCP sync, SHA-256 file indexing, file watching, manual peer connection, and LAN discovery.

Build it with Qt 6 or Qt 5.15:

```bash
cmake -S clients/qt-lansync -B clients/qt-lansync/build
cmake --build clients/qt-lansync/build
```

### Unit Test

```bash
go test -v ./internal/...

```

