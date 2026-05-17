#pragma once

#include "filesystem/FileWatcher.h"
#include "network/MdnsDiscovery.h"
#include "network/Transport.h"

#include <QDateTime>
#include <QHash>
#include <QHostAddress>
#include <QObject>
#include <QStringList>

struct PeerEntry {
    QString peerId;
    QString addr;
    QString hostname;
    QString status;
    QDateTime lastSeen;
};

class SyncEngine : public QObject {
    Q_OBJECT

public:
    explicit SyncEngine(QObject *parent = nullptr);

    bool start(const QString &dir, quint16 port, const QHostAddress &bindAddr = QHostAddress::Any);
    void stop();
    void connectTo(const QString &addr);
    void resendIndex();

    QString myId() const;
    QString watchDir() const;
    quint16 port() const;
    QList<PeerEntry> peers() const;
    int connectedCount() const;
    int syncedFiles() const;
    int sentFiles() const;
    int requestedFiles() const;
    bool isRunning() const;

signals:
    void stateChanged();
    void logAdded(const QString &message, const QString &level);

private:
    void handlePeerConnected(const QString &peerId, const QString &addr);
    void handlePeerDisconnected(const QString &peerId);
    void handleMessage(const QString &peerId, const SyncMessage &message);
    void handleLocalChange(const SyncMessage &message);
    void handleDiscoveredPeer(const QString &addr, const QString &hostname);
    void handleRecvNotify(const QString &peerId, const SyncMessage &message);
    void handleRecvPullRequest(const QString &peerId, const SyncMessage &message);
    void handleRecvFileData(const SyncMessage &message);
    void sendFullIndex(const QString &peerId);
    void addLog(const QString &message, const QString &level);
    static QString shortId(const QString &id, int n = 12);

    Transport m_transport;
    FileWatcher m_watcher;
    MdnsDiscovery m_discovery;
    QString m_watchDir;
    QHash<QString, PeerEntry> m_peers;
    QStringList m_peerOrder;
    QSet<QString> m_indexSent;
    int m_syncedFiles = 0;
    int m_sentFiles = 0;
    int m_requestedFiles = 0;
    bool m_running = false;
};
