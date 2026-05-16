#pragma once

#include "protocol/Protocol.h"

#include <QHash>
#include <QObject>
#include <QTcpServer>
#include <QTcpSocket>

class Transport : public QObject {
    Q_OBJECT

public:
    explicit Transport(QObject *parent = nullptr);
    ~Transport() override;

    bool start(quint16 port);
    void stop();
    void connectTo(const QString &addr);
    bool sendTo(const QString &peerId, const SyncMessage &message);
    void broadcast(const SyncMessage &message);

    QString myId() const;
    quint16 port() const;
    QStringList peers() const;

signals:
    void peerConnected(const QString &peerId, const QString &addr);
    void peerDisconnected(const QString &peerId);
    void messageReceived(const QString &peerId, const SyncMessage &message);
    void transportLog(const QString &message, const QString &level);

private:
    enum class Direction {
        Dial,
        Accept,
    };

    struct PeerConn {
        QTcpSocket *socket = nullptr;
        Direction direction = Direction::Dial;
    };

    struct PendingConn {
        QString addr;
        Direction direction = Direction::Dial;
        QByteArray buffer;
        bool sentHandshake = false;
    };

    void handleIncoming();
    void setupPending(QTcpSocket *socket, const QString &addr, Direction direction);
    void sendHandshake(QTcpSocket *socket);
    void consumePending(QTcpSocket *socket);
    void consumeEstablished(const QString &peerId);
    void handleHandshakeMessage(QTcpSocket *socket, const SyncMessage &message);
    bool registerPeer(const QString &peerId, QTcpSocket *socket, Direction direction);
    void finishPeer(const QString &peerId, QTcpSocket *socket);
    void closeSocket(QTcpSocket *socket);
    void writeMessage(QTcpSocket *socket, const SyncMessage &message);
    static QList<SyncMessage> takeMessages(QByteArray &buffer);
    static QString newPeerId();

    QTcpServer m_server;
    QString m_myId;
    quint16 m_port = 0;
    QHash<QString, PeerConn> m_peers;
    QHash<QTcpSocket *, PendingConn> m_pending;
    QHash<QTcpSocket *, QString> m_socketToPeer;
};
