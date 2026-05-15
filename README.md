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

### Unit Test

```bash
go test -v ./internal/...

```

