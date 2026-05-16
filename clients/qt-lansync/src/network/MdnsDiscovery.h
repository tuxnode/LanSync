#pragma once

#include <QHash>
#include <QHostAddress>
#include <QObject>
#include <QSet>
#include <QTcpSocket>
#include <QTimer>
#include <QUdpSocket>

class MdnsDiscovery : public QObject {
    Q_OBJECT

public:
    explicit MdnsDiscovery(QObject *parent = nullptr);
    bool start(quint16 port, const QString &peerId);
    void stop();

signals:
    void peerDiscovered(const QString &addr, const QString &hostname);
    void discoveryLog(const QString &message, const QString &level);

private:
    struct Discovered {
        QString hostname;
        QHostAddress ip;
        quint16 port = 0;
    };

    void sendQuery();
    void sendAnnouncement();
    void startIpScanner();
    void scanLocalNetworks();
    void scanTarget(const QHostAddress &ip);
    void emitDiscovered(const QString &addr, const QString &hostname);
    void readDatagrams();
    QByteArray buildQuery() const;
    QByteArray buildResponse() const;
    void appendName(QByteArray &out, const QString &name) const;
    void appendRecord(QByteArray &out, const QString &name, quint16 type, const QByteArray &rdata) const;
    QString readName(const QByteArray &packet, int &offset) const;
    QList<Discovered> parseResponse(const QByteArray &packet, const QHostAddress &fallbackIp) const;
    bool isQueryForService(const QByteArray &packet) const;
    QHostAddress localIPv4() const;
    static quint16 readU16(const QByteArray &packet, int offset);
    static quint32 readU32(const QByteArray &packet, int offset);
    static QString sanitizeLabel(const QString &label);

    QUdpSocket m_socket;
    QTimer *m_queryTimer = nullptr;
    QTimer *m_announceTimer = nullptr;
    QTimer *m_scanTimer = nullptr;
    quint16 m_port = 0;
    QString m_instance;
    QString m_hostName;
    QHostAddress m_advertisedIp;
    QHash<QString, QDateTime> m_recent;
    QSet<QTcpSocket *> m_scanSockets;
};
