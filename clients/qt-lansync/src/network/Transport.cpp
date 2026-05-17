#include "network/Transport.h"

#include <QDateTime>
#include <QHostAddress>
#include <QJsonDocument>
#include <QRandomGenerator>

Transport::Transport(QObject *parent)
    : QObject(parent)
    , m_myId(newPeerId())
{
    connect(&m_server, &QTcpServer::newConnection, this, &Transport::handleIncoming);
}

Transport::~Transport()
{
    stop();
}

bool Transport::start(quint16 port, const QHostAddress &bindAddr)
{
    if (m_server.isListening()) {
        return true;
    }
    if (!m_server.listen(bindAddr, port)) {
        emit transportLog(QStringLiteral("启动 TCP 监听失败: %1").arg(m_server.errorString()), QStringLiteral("err"));
        return false;
    }
    m_port = m_server.serverPort();
    return true;
}

void Transport::stop()
{
    if (m_server.isListening()) {
        m_server.close();
    }

    const auto sockets = m_socketToPeer.keys() + m_pending.keys();
    for (QTcpSocket *socket : sockets) {
        closeSocket(socket);
    }
    m_peers.clear();
    m_pending.clear();
    m_socketToPeer.clear();
}

void Transport::connectTo(const QString &addr)
{
    const int colon = addr.lastIndexOf(':');
    if (colon <= 0) {
        emit transportLog(QStringLiteral("连接地址无效: %1").arg(addr), QStringLiteral("err"));
        return;
    }

    const QString host = addr.left(colon);
    bool ok = false;
    const quint16 port = addr.mid(colon + 1).toUShort(&ok);
    if (!ok || port == 0) {
        emit transportLog(QStringLiteral("连接端口无效: %1").arg(addr), QStringLiteral("err"));
        return;
    }

    auto *socket = new QTcpSocket(this);
    setupPending(socket, addr, Direction::Dial);
    connect(socket, &QTcpSocket::connected, this, [this, socket]() {
        sendHandshake(socket);
    });
    connect(socket, qOverload<QAbstractSocket::SocketError>(&QTcpSocket::errorOccurred), this, [this, socket](QAbstractSocket::SocketError) {
        if (m_pending.contains(socket)) {
            emit transportLog(QStringLiteral("连接失败 [%1]: %2").arg(m_pending.value(socket).addr, socket->errorString()), QStringLiteral("err"));
        }
        closeSocket(socket);
    });
    socket->connectToHost(host, port);
}

bool Transport::sendTo(const QString &peerId, const SyncMessage &message)
{
    auto it = m_peers.find(peerId);
    if (it == m_peers.end() || !it->socket) {
        return false;
    }
    writeMessage(it->socket, message);
    return true;
}

void Transport::broadcast(const SyncMessage &message)
{
    const QStringList ids = peers();
    for (const QString &peerId : ids) {
        sendTo(peerId, message);
    }
}

QString Transport::myId() const
{
    return m_myId;
}

quint16 Transport::port() const
{
    return m_port;
}

QStringList Transport::peers() const
{
    return m_peers.keys();
}

void Transport::handleIncoming()
{
    while (QTcpSocket *socket = m_server.nextPendingConnection()) {
        setupPending(socket, socket->peerAddress().toString() + ":" + QString::number(socket->peerPort()), Direction::Accept);
    }
}

void Transport::setupPending(QTcpSocket *socket, const QString &addr, Direction direction)
{
    PendingConn pending;
    pending.addr = addr;
    pending.direction = direction;
    m_pending.insert(socket, pending);

    connect(socket, &QTcpSocket::readyRead, this, [this, socket]() {
        if (m_pending.contains(socket)) {
            consumePending(socket);
            return;
        }
        const QString peerId = m_socketToPeer.value(socket);
        if (!peerId.isEmpty()) {
            consumeEstablished(peerId);
        }
    });
    connect(socket, &QTcpSocket::disconnected, this, [this, socket]() {
        closeSocket(socket);
    });
}

void Transport::sendHandshake(QTcpSocket *socket)
{
    if (!m_pending.contains(socket)) {
        return;
    }
    auto pending = m_pending.value(socket);
    pending.sentHandshake = true;
    m_pending.insert(socket, pending);
    writeMessage(socket, SyncMessage::handshake(m_myId, QDateTime::currentMSecsSinceEpoch()));
}

void Transport::consumePending(QTcpSocket *socket)
{
    auto pending = m_pending.value(socket);
    pending.buffer += socket->readAll();
    const QList<SyncMessage> messages = takeMessages(pending.buffer);
    m_pending.insert(socket, pending);

    for (const SyncMessage &message : messages) {
        handleHandshakeMessage(socket, message);
        if (!m_pending.contains(socket)) {
            break;
        }
    }
}

void Transport::consumeEstablished(const QString &peerId)
{
    auto it = m_peers.find(peerId);
    if (it == m_peers.end() || !it->socket) {
        return;
    }
    QTcpSocket *socket = it->socket;
    QByteArray buffer = socket->property("buffer").toByteArray();
    buffer += socket->readAll();
    const QList<SyncMessage> messages = takeMessages(buffer);
    socket->setProperty("buffer", buffer);

    for (const SyncMessage &message : messages) {
        const MessageType kind = message.kind();
        if (kind == MessageType::HandShake || kind == MessageType::HandShakeReject) {
            continue;
        }
        emit messageReceived(peerId, message);
    }
}

void Transport::handleHandshakeMessage(QTcpSocket *socket, const SyncMessage &message)
{
    if (!m_pending.contains(socket)) {
        return;
    }
    PendingConn pending = m_pending.value(socket);

    if (message.kind() == MessageType::HandShakeReject) {
        emit transportLog(QStringLiteral("握手被对端拒绝: %1").arg(pending.addr), QStringLiteral("warn"));
        closeSocket(socket);
        return;
    }
    if (message.kind() != MessageType::HandShake || message.peerId.isEmpty()) {
        emit transportLog(QStringLiteral("握手消息无效: %1").arg(pending.addr), QStringLiteral("err"));
        closeSocket(socket);
        return;
    }

    if (pending.direction == Direction::Accept && !pending.sentHandshake) {
        sendHandshake(socket);
    }

    if (!registerPeer(message.peerId, socket, pending.direction)) {
        writeMessage(socket, SyncMessage::make(MessageType::HandShakeReject));
        closeSocket(socket);
        return;
    }

    m_pending.remove(socket);
    m_socketToPeer.insert(socket, message.peerId);
    socket->setProperty("buffer", pending.buffer);
    emit peerConnected(message.peerId, pending.addr);
}

bool Transport::registerPeer(const QString &peerId, QTcpSocket *socket, Direction direction)
{
    if (m_peers.contains(peerId)) {
        const PeerConn existing = m_peers.value(peerId);
        if (existing.direction == direction) {
            return false;
        }

        bool keepExisting = false;
        if (direction == Direction::Dial) {
            if (m_myId < peerId) {
                closeSocket(existing.socket);
            } else {
                keepExisting = true;
            }
        } else {
            if (m_myId > peerId) {
                closeSocket(existing.socket);
            } else {
                keepExisting = true;
            }
        }

        if (keepExisting) {
            return false;
        }
    }

    PeerConn peer;
    peer.socket = socket;
    peer.direction = direction;
    m_peers.insert(peerId, peer);
    return true;
}

void Transport::finishPeer(const QString &peerId, QTcpSocket *socket)
{
    if (peerId.isEmpty()) {
        return;
    }
    auto it = m_peers.find(peerId);
    if (it != m_peers.end() && it->socket == socket) {
        m_peers.erase(it);
        emit peerDisconnected(peerId);
    }
}

void Transport::closeSocket(QTcpSocket *socket)
{
    if (!socket) {
        return;
    }

    const QString peerId = m_socketToPeer.take(socket);
    finishPeer(peerId, socket);
    m_pending.remove(socket);
    socket->disconnect(this);
    socket->abort();
    socket->deleteLater();
}

void Transport::writeMessage(QTcpSocket *socket, const SyncMessage &message)
{
    if (!socket || socket->state() == QAbstractSocket::UnconnectedState) {
        return;
    }
    const QByteArray payload = QJsonDocument(message.toJson()).toJson(QJsonDocument::Compact) + '\n';
    socket->write(payload);
    socket->flush();
}

QList<SyncMessage> Transport::takeMessages(QByteArray &buffer)
{
    QList<SyncMessage> messages;
    while (true) {
        const int newline = buffer.indexOf('\n');
        if (newline < 0) {
            break;
        }
        const QByteArray line = buffer.left(newline).trimmed();
        buffer.remove(0, newline + 1);
        if (line.isEmpty()) {
            continue;
        }
        const QJsonDocument doc = QJsonDocument::fromJson(line);
        if (!doc.isObject()) {
            continue;
        }
        messages.append(SyncMessage::fromJson(doc.object()));
    }
    return messages;
}

QString Transport::newPeerId()
{
    QByteArray value(16, Qt::Uninitialized);
    const quint64 ms = static_cast<quint64>(QDateTime::currentMSecsSinceEpoch());
    value[0] = static_cast<char>((ms >> 40) & 0xff);
    value[1] = static_cast<char>((ms >> 32) & 0xff);
    value[2] = static_cast<char>((ms >> 24) & 0xff);
    value[3] = static_cast<char>((ms >> 16) & 0xff);
    value[4] = static_cast<char>((ms >> 8) & 0xff);
    value[5] = static_cast<char>(ms & 0xff);
    for (int i = 6; i < 16; ++i) {
        value[i] = static_cast<char>(QRandomGenerator::global()->generate() & 0xff);
    }
    value[6] = static_cast<char>((value[6] & 0x0f) | 0x70);
    value[8] = static_cast<char>((value[8] & 0x3f) | 0x80);

    const QString hex = QString::fromLatin1(value.toHex());
    return hex.mid(0, 8) + "-" + hex.mid(8, 4) + "-" + hex.mid(12, 4) + "-" + hex.mid(16, 4) + "-" + hex.mid(20, 12);
}
