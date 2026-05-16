#include "core/SyncEngine.h"

#include "filesystem/FileIndexer.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>

SyncEngine::SyncEngine(QObject *parent)
    : QObject(parent)
{
    connect(&m_transport, &Transport::peerConnected, this, &SyncEngine::handlePeerConnected);
    connect(&m_transport, &Transport::peerDisconnected, this, &SyncEngine::handlePeerDisconnected);
    connect(&m_transport, &Transport::messageReceived, this, &SyncEngine::handleMessage);
    connect(&m_transport, &Transport::transportLog, this, &SyncEngine::addLog);
    connect(&m_watcher, &FileWatcher::fileChangedMessage, this, &SyncEngine::handleLocalChange);
    connect(&m_watcher, &FileWatcher::watcherLog, this, &SyncEngine::addLog);
    connect(&m_discovery, &MdnsDiscovery::peerDiscovered, this, &SyncEngine::handleDiscoveredPeer);
    connect(&m_discovery, &MdnsDiscovery::discoveryLog, this, &SyncEngine::addLog);
}

bool SyncEngine::start(const QString &dir, quint16 port)
{
    stop();
    m_watchDir = QDir(dir).absolutePath();
    QDir().mkpath(m_watchDir);

    if (!m_transport.start(port)) {
        return false;
    }
    m_watcher.start(m_watchDir);
    m_discovery.start(m_transport.port(), m_transport.myId());
    m_running = true;

    addLog(QStringLiteral("LanSync Qt 已启动，监听目录: %1").arg(m_watchDir), QStringLiteral("info"));
    addLog(QStringLiteral("本机 ID: %1, TCP 端口: %2").arg(shortId(m_transport.myId()), QString::number(m_transport.port())), QStringLiteral("info"));
    emit stateChanged();
    return true;
}

void SyncEngine::stop()
{
    if (!m_running && !m_transport.port()) {
        return;
    }
    m_transport.broadcast(SyncMessage::make(MessageType::Bye));
    m_discovery.stop();
    m_watcher.stop();
    m_transport.stop();
    m_indexSent.clear();
    for (auto it = m_peers.begin(); it != m_peers.end(); ++it) {
        it->status = QStringLiteral("离线");
        it->lastSeen = QDateTime::currentDateTimeUtc();
    }
    m_running = false;
    emit stateChanged();
}

void SyncEngine::connectTo(const QString &addr)
{
    addLog(QStringLiteral("正在连接: %1").arg(addr), QStringLiteral("conn"));
    m_transport.connectTo(addr);
}

void SyncEngine::resendIndex()
{
    m_indexSent.clear();
    const QStringList peerIds = m_transport.peers();
    for (const QString &peerId : peerIds) {
        sendFullIndex(peerId);
    }
}

QString SyncEngine::myId() const
{
    return m_transport.myId();
}

QString SyncEngine::watchDir() const
{
    return m_watchDir;
}

quint16 SyncEngine::port() const
{
    return m_transport.port();
}

QList<PeerEntry> SyncEngine::peers() const
{
    QList<PeerEntry> out;
    for (const QString &peerId : m_peerOrder) {
        if (m_peers.contains(peerId)) {
            out << m_peers.value(peerId);
        }
    }
    return out;
}

int SyncEngine::connectedCount() const
{
    int count = 0;
    for (const PeerEntry &peer : m_peers) {
        if (peer.status == QStringLiteral("已连接")) {
            ++count;
        }
    }
    return count;
}

int SyncEngine::syncedFiles() const
{
    return m_syncedFiles;
}

int SyncEngine::sentFiles() const
{
    return m_sentFiles;
}

int SyncEngine::requestedFiles() const
{
    return m_requestedFiles;
}

bool SyncEngine::isRunning() const
{
    return m_running;
}

void SyncEngine::handlePeerConnected(const QString &peerId, const QString &addr)
{
    PeerEntry peer;
    peer.peerId = peerId;
    peer.addr = addr;
    peer.hostname = addr.section(':', 0, 0);
    peer.status = QStringLiteral("已连接");
    peer.lastSeen = QDateTime::currentDateTimeUtc();

    if (!m_peers.contains(peerId)) {
        m_peerOrder << peerId;
    }
    m_peers.insert(peerId, peer);

    addLog(QStringLiteral("新节点接入: %1 (%2)").arg(shortId(peerId), addr), QStringLiteral("conn"));
    sendFullIndex(peerId);
    emit stateChanged();
}

void SyncEngine::handlePeerDisconnected(const QString &peerId)
{
    m_indexSent.remove(peerId);
    if (m_peers.contains(peerId)) {
        auto peer = m_peers.value(peerId);
        peer.status = QStringLiteral("离线");
        peer.lastSeen = QDateTime::currentDateTimeUtc();
        m_peers.insert(peerId, peer);
    }
    addLog(QStringLiteral("节点离开: %1").arg(shortId(peerId)), QStringLiteral("warn"));
    emit stateChanged();
}

void SyncEngine::handleMessage(const QString &peerId, const SyncMessage &message)
{
    switch (message.kind()) {
    case MessageType::Notify:
        addLog(QStringLiteral("← %1 收到变更: %2").arg(shortId(peerId, 10), message.relPath), QStringLiteral("sync"));
        handleRecvNotify(peerId, message);
        break;
    case MessageType::PullRequest:
        addLog(QStringLiteral("← %1 请求下载: %2").arg(shortId(peerId, 10), message.relPath), QStringLiteral("info"));
        handleRecvPullRequest(peerId, message);
        break;
    case MessageType::FileData:
        addLog(QStringLiteral("← %1 接收文件: %2").arg(shortId(peerId, 10), message.relPath), QStringLiteral("sync"));
        handleRecvFileData(message);
        break;
    case MessageType::Error:
        addLog(QStringLiteral("← %1 错误: %2 %3").arg(shortId(peerId, 10), message.relPath, message.data), QStringLiteral("err"));
        break;
    case MessageType::Bye:
        handlePeerDisconnected(peerId);
        break;
    default:
        break;
    }
}

void SyncEngine::handleLocalChange(const SyncMessage &message)
{
    m_transport.broadcast(message);
    addLog(QStringLiteral("→ 推送变更: %1").arg(message.relPath), QStringLiteral("sync"));
}

void SyncEngine::handleDiscoveredPeer(const QString &addr, const QString &hostname)
{
    for (const PeerEntry &peer : m_peers) {
        if (peer.addr == addr && peer.status == QStringLiteral("已连接")) {
            return;
        }
    }
    addLog(QStringLiteral("发现节点: %1 (%2)").arg(hostname, addr), QStringLiteral("conn"));
    m_transport.connectTo(addr);
}

void SyncEngine::handleRecvNotify(const QString &peerId, const SyncMessage &message)
{
    const QString localPath = FileIndexer::safeJoin(m_watchDir, message.relPath);
    if (localPath.isEmpty()) {
        addLog(QStringLiteral("拒绝不安全路径: %1").arg(message.relPath), QStringLiteral("err"));
        return;
    }

    if (QFileInfo::exists(localPath) && QFileInfo(localPath).isFile()) {
        const QString localHash = FileIndexer::calculateHash(localPath);
        if (!localHash.isEmpty() && localHash == message.hash) {
            return;
        }
    }

    if (m_transport.sendTo(peerId, SyncMessage::pullRequest(message.relPath))) {
        ++m_requestedFiles;
        addLog(QStringLiteral("→ 请求下载: %1").arg(message.relPath), QStringLiteral("sync"));
        emit stateChanged();
    }
}

void SyncEngine::handleRecvPullRequest(const QString &peerId, const SyncMessage &message)
{
    const QString localPath = FileIndexer::safeJoin(m_watchDir, message.relPath);
    QFile file(localPath);
    if (localPath.isEmpty() || !file.open(QIODevice::ReadOnly)) {
        m_transport.sendTo(peerId, SyncMessage::error(message.relPath, QStringLiteral("读取文件失败")));
        return;
    }

    const QByteArray data = file.readAll();
    const QString encoded = QString::fromLatin1(data.toBase64());
    if (m_transport.sendTo(peerId, SyncMessage::fileData(message.relPath, data.size(), encoded))) {
        ++m_sentFiles;
        addLog(QStringLiteral("→ 发送文件: %1 (%2 bytes)").arg(message.relPath, QString::number(data.size())), QStringLiteral("sync"));
        emit stateChanged();
    }
}

void SyncEngine::handleRecvFileData(const SyncMessage &message)
{
    const QString localPath = FileIndexer::safeJoin(m_watchDir, message.relPath);
    if (localPath.isEmpty()) {
        addLog(QStringLiteral("拒绝写入不安全路径: %1").arg(message.relPath), QStringLiteral("err"));
        return;
    }

    const QByteArray data = QByteArray::fromBase64(message.data.toLatin1());
    QDir().mkpath(QFileInfo(localPath).absolutePath());
    m_watcher.addIgnorePath(localPath);

    QFile file(localPath);
    if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate)) {
        addLog(QStringLiteral("写入文件失败: %1").arg(message.relPath), QStringLiteral("err"));
        return;
    }
    file.write(data);
    file.close();

    ++m_syncedFiles;
    addLog(QStringLiteral("文件已同步: %1 (%2 bytes)").arg(message.relPath, QString::number(data.size())), QStringLiteral("sync"));
    emit stateChanged();
}

void SyncEngine::sendFullIndex(const QString &peerId)
{
    if (m_indexSent.contains(peerId)) {
        return;
    }
    m_indexSent.insert(peerId);

    const IndexMap index = FileIndexer::generateIndex(m_watchDir);
    int count = 0;
    for (const FileInfo &info : index) {
        if (info.isFolder) {
            continue;
        }
        if (!m_transport.sendTo(peerId, SyncMessage::notify(info.relPath, info.hash, info.size, info.modTime))) {
            addLog(QStringLiteral("发送索引中断: %1").arg(info.relPath), QStringLiteral("err"));
            return;
        }
        ++count;
    }
    addLog(QStringLiteral("已发送完整索引至 %1: %2 个文件").arg(shortId(peerId), QString::number(count)), QStringLiteral("conn"));
}

void SyncEngine::addLog(const QString &message, const QString &level)
{
    emit logAdded(message, level);
}

QString SyncEngine::shortId(const QString &id, int n)
{
    if (id.size() <= n) {
        return id;
    }
    return id.left(n) + QStringLiteral("...");
}
