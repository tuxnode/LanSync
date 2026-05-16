# LanSync Qt

`qt-lansync` 是 LanSync 的 Qt Widgets 桌面版本，目标是在 Linux 和 Windows 上使用同一套 CMake 工程编译运行。

## 功能

- GUI 配置同步目录和 TCP 端口。
- 手动连接 `host:port` 节点。
- 节点表、状态栏和事件日志。
- TCP JSON 协议兼容 Go/Rust 版本的 `SyncMessage`。
- SHA-256 文件索引、hash 比较、按需拉取文件。
- `QFileSystemWatcher` 跨平台监听目录变化。
- UDP mDNS/DNS-SD 自动发现；若系统防火墙或 5353 端口限制导致失败，仍可手动连接。

## Linux 构建

安装 Qt 6 或 Qt 5.15 开发包、CMake 和 C++17 编译器后：

```bash
cd qt-lansync
cmake -S . -B build
cmake --build build -j
./build/qt-lansync
```

本项目优先查找 Qt 6，找不到时回退 Qt 5：

```cmake
find_package(Qt6 QUIET COMPONENTS Widgets Network)
find_package(Qt5 REQUIRED COMPONENTS Widgets Network)
```

也可以强制选择 Qt 主版本：

```bash
cmake -S . -B build-qt6 -DLANSYNC_QT_MAJOR=6
cmake -S . -B build-qt5 -DLANSYNC_QT_MAJOR=5
```

## Windows 构建

推荐使用 Qt Creator：

1. 安装 Qt 6 或 Qt 5.15，选择 MSVC 或 MinGW kit。
2. 用 Qt Creator 打开 `qt-lansync/CMakeLists.txt`。
3. Configure Project。
4. Build 后运行 `qt-lansync.exe`。

命令行构建示例，需先进入 Qt 对应编译器环境：

```powershell
cmake -S . -B build -G "Ninja"
cmake --build build
.\build\qt-lansync.exe
```

如果使用 MSVC，先打开 “x64 Native Tools Command Prompt for VS”；如果使用 MinGW，确保 Qt 的 `bin` 和 MinGW `bin` 在 `PATH` 中。

## 无显示环境检查

```bash
./build/qt-lansync --version
```

真正运行 GUI 仍需要 Linux 桌面会话或 Windows 图形会话。
