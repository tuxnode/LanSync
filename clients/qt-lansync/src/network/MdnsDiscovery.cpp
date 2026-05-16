#include "network/MdnsDiscovery.h"

#include <QDateTime>
#include <QHostInfo>
#include <QNetworkDatagram>
#include <QNetworkInterface>
#include <QTimer>

namespace {
constexpr quint16 MdnsPort = 5353;
constexpr quint16 TypeA = 1;
constexpr quint16 TypePtr = 12;
constexpr quint16 TypeTxt = 16;
constexpr quint16 TypeSrv = 33;
constexpr quint16 ClassIn = 1;
const QString Service = QStringLiteral("_lansync._tcp.local");
const QHostAddress Multicast(QStringLiteral("224.0.0.251"));
constexpr int IpScanIntervalMs = 15000;
constexpr int IpScanTimeoutMs = 700;
constexpr int MaxScanTargets = 512;
}

MdnsDiscovery::MdnsDiscovery(QObject *parent)
    : QObject(parent)
{
    connect(&m_socket, &QUdpSocket::readyRead, this, &MdnsDiscovery::readDatagrams);
}

bool MdnsDiscovery::start(quint16 port, const QString &peerId)
{
    stop();

    m_port = port;
    m_advertisedIp = localIPv4();
    const QString baseHost = sanitizeLabel(QHostInfo::localHostName().isEmpty() ? QStringLiteral("lansync-node") : QHostInfo::localHostName());
    m_hostName = baseHost + QStringLiteral(".local");
    m_instance = baseHost + "-" + peerId.left(8) + "." + Service;

#if QT_VERSION >= QT_VERSION_CHECK(5, 8, 0)
    const bool bound = m_socket.bind(QHostAddress::AnyIPv4, MdnsPort, QUdpSocket::ShareAddress | QUdpSocket::ReuseAddressHint);
#else
    const bool bound = m_socket.bind(QHostAddress::Any, MdnsPort, QUdpSocket::ShareAddress | QUdpSocket::ReuseAddressHint);
#endif
    if (!bound) {
        emit discoveryLog(QStringLiteral("mDNS 绑定失败: %1").arg(m_socket.errorString()), QStringLiteral("warn"));
        startIpScanner();
        return true;
    }

    if (!m_socket.joinMulticastGroup(Multicast)) {
        emit discoveryLog(QStringLiteral("mDNS 加入组播失败: %1").arg(m_socket.errorString()), QStringLiteral("warn"));
        m_socket.close();
        startIpScanner();
        return true;
    }

    m_queryTimer = new QTimer(this);
    m_queryTimer->setInterval(3000);
    connect(m_queryTimer, &QTimer::timeout, this, &MdnsDiscovery::sendQuery);
    m_queryTimer->start();

    m_announceTimer = new QTimer(this);
    m_announceTimer->setInterval(10000);
    connect(m_announceTimer, &QTimer::timeout, this, &MdnsDiscovery::sendAnnouncement);
    m_announceTimer->start();

    sendQuery();
    sendAnnouncement();
    startIpScanner();
    return true;
}

void MdnsDiscovery::stop()
{
    if (m_queryTimer) {
        m_queryTimer->deleteLater();
        m_queryTimer = nullptr;
    }
    if (m_announceTimer) {
        m_announceTimer->deleteLater();
        m_announceTimer = nullptr;
    }
    if (m_scanTimer) {
        m_scanTimer->deleteLater();
        m_scanTimer = nullptr;
    }
    const auto sockets = m_scanSockets;
    for (QTcpSocket *socket : sockets) {
        socket->disconnect(this);
        socket->abort();
        socket->deleteLater();
    }
    m_scanSockets.clear();
    if (m_socket.state() != QAbstractSocket::UnconnectedState) {
        m_socket.leaveMulticastGroup(Multicast);
        m_socket.close();
    }
    m_port = 0;
}

void MdnsDiscovery::sendQuery()
{
    const QByteArray packet = buildQuery();
    m_socket.writeDatagram(packet, Multicast, MdnsPort);
}

void MdnsDiscovery::sendAnnouncement()
{
    const QByteArray packet = buildResponse();
    m_socket.writeDatagram(packet, Multicast, MdnsPort);
}

void MdnsDiscovery::startIpScanner()
{
    if (m_scanTimer) {
        return;
    }

    m_scanTimer = new QTimer(this);
    m_scanTimer->setInterval(IpScanIntervalMs);
    connect(m_scanTimer, &QTimer::timeout, this, &MdnsDiscovery::scanLocalNetworks);
    m_scanTimer->start();

    QTimer::singleShot(500, this, &MdnsDiscovery::scanLocalNetworks);
    emit discoveryLog(QStringLiteral("已启用局域网 IP 扫描兜底，端口: %1").arg(m_port), QStringLiteral("info"));
}

void MdnsDiscovery::scanLocalNetworks()
{
    if (m_port == 0 || !m_scanSockets.isEmpty()) {
        return;
    }

    QSet<quint32> localIps;
    QSet<quint32> targets;
    const QList<QNetworkInterface> interfaces = QNetworkInterface::allInterfaces();
    for (const QNetworkInterface &iface : interfaces) {
        const auto flags = iface.flags();
        if (!flags.testFlag(QNetworkInterface::IsUp)
            || !flags.testFlag(QNetworkInterface::IsRunning)
            || flags.testFlag(QNetworkInterface::IsLoopBack)) {
            continue;
        }

        const QList<QNetworkAddressEntry> entries = iface.addressEntries();
        for (const QNetworkAddressEntry &entry : entries) {
            const QHostAddress ip = entry.ip();
            if (ip.protocol() != QAbstractSocket::IPv4Protocol || ip.isLoopback()) {
                continue;
            }

            const quint32 local = ip.toIPv4Address();
            localIps.insert(local);

            const quint32 network = local & 0xffffff00U;
            for (quint32 host = 1; host < 255; ++host) {
                targets.insert(network | host);
            }
        }
    }

    int launched = 0;
    for (const quint32 target : targets) {
        if (localIps.contains(target)) {
            continue;
        }
        scanTarget(QHostAddress(target));
        if (++launched >= MaxScanTargets) {
            break;
        }
    }
}

void MdnsDiscovery::scanTarget(const QHostAddress &ip)
{
    auto *socket = new QTcpSocket(this);
    m_scanSockets.insert(socket);

    const QString host = ip.toString();
    const QString addr = host + ":" + QString::number(m_port);

    connect(socket, &QObject::destroyed, this, [this, socket]() {
        m_scanSockets.remove(socket);
    });
    connect(socket, &QTcpSocket::connected, this, [this, socket, addr, host]() {
        emitDiscovered(addr, QStringLiteral("ip-scan-%1").arg(host));
        socket->abort();
        socket->deleteLater();
    });
    connect(socket, qOverload<QAbstractSocket::SocketError>(&QTcpSocket::errorOccurred), socket, &QObject::deleteLater);
    QTimer::singleShot(IpScanTimeoutMs, socket, [socket]() {
        if (socket->state() != QAbstractSocket::UnconnectedState) {
            socket->abort();
        }
        socket->deleteLater();
    });

    socket->connectToHost(ip, m_port);
}

void MdnsDiscovery::emitDiscovered(const QString &addr, const QString &hostname)
{
    const QDateTime now = QDateTime::currentDateTimeUtc();
    if (m_recent.contains(addr) && m_recent.value(addr).msecsTo(now) < 10000) {
        return;
    }
    m_recent.insert(addr, now);
    emit peerDiscovered(addr, hostname);
}

void MdnsDiscovery::readDatagrams()
{
    while (m_socket.hasPendingDatagrams()) {
        const QNetworkDatagram datagram = m_socket.receiveDatagram();
        const QByteArray packet = datagram.data();
        if (isQueryForService(packet)) {
            sendAnnouncement();
            continue;
        }

        const QList<Discovered> peers = parseResponse(packet, datagram.senderAddress());
        for (const Discovered &peer : peers) {
            if (peer.ip == m_advertisedIp && peer.port == m_port) {
                continue;
            }
            const QString addr = peer.ip.toString() + ":" + QString::number(peer.port);
            emitDiscovered(addr, peer.hostname);
        }
    }
}

QByteArray MdnsDiscovery::buildQuery() const
{
    QByteArray out;
    out.append(QByteArray::fromHex("000000000001000000000000"));
    appendName(out, Service);
    out.append(char(TypePtr >> 8));
    out.append(char(TypePtr & 0xff));
    out.append(char(ClassIn >> 8));
    out.append(char(ClassIn & 0xff));
    return out;
}

QByteArray MdnsDiscovery::buildResponse() const
{
    QByteArray out;
    out.append(QByteArray::fromHex("000084000000000400000000"));

    QByteArray ptr;
    appendName(ptr, m_instance);
    appendRecord(out, Service, TypePtr, ptr);

    QByteArray srv;
    srv.append(QByteArray::fromHex("00000000"));
    srv.append(char(m_port >> 8));
    srv.append(char(m_port & 0xff));
    appendName(srv, m_hostName);
    appendRecord(out, m_instance, TypeSrv, srv);

    QByteArray a;
    const quint32 ip = m_advertisedIp.toIPv4Address();
    a.append(char((ip >> 24) & 0xff));
    a.append(char((ip >> 16) & 0xff));
    a.append(char((ip >> 8) & 0xff));
    a.append(char(ip & 0xff));
    appendRecord(out, m_hostName, TypeA, a);

    QByteArray txt;
    txt.append(char(5));
    txt.append("v=1.0");
    appendRecord(out, m_instance, TypeTxt, txt);
    return out;
}

void MdnsDiscovery::appendName(QByteArray &out, const QString &name) const
{
    const QStringList parts = name.split('.', Qt::SkipEmptyParts);
    for (const QString &part : parts) {
        const QByteArray label = part.toUtf8().left(63);
        out.append(char(label.size()));
        out.append(label);
    }
    out.append(char(0));
}

void MdnsDiscovery::appendRecord(QByteArray &out, const QString &name, quint16 type, const QByteArray &rdata) const
{
    appendName(out, name);
    out.append(char(type >> 8));
    out.append(char(type & 0xff));
    out.append(char(ClassIn >> 8));
    out.append(char(ClassIn & 0xff));
    out.append(QByteArray::fromHex("00000078"));
    out.append(char(rdata.size() >> 8));
    out.append(char(rdata.size() & 0xff));
    out.append(rdata);
}

QString MdnsDiscovery::readName(const QByteArray &packet, int &offset) const
{
    QStringList labels;
    int cursor = offset;
    int next = offset;
    bool jumped = false;

    for (int depth = 0; depth < 128; ++depth) {
        if (cursor < 0 || cursor >= packet.size()) {
            return {};
        }
        const quint8 len = static_cast<quint8>(packet.at(cursor));
        if ((len & 0xc0) == 0xc0) {
            if (cursor + 1 >= packet.size()) {
                return {};
            }
            const int ptr = ((len & 0x3f) << 8) | static_cast<quint8>(packet.at(cursor + 1));
            if (!jumped) {
                next = cursor + 2;
            }
            cursor = ptr;
            jumped = true;
            continue;
        }
        if (len == 0) {
            if (!jumped) {
                next = cursor + 1;
            }
            offset = next;
            return labels.join('.');
        }
        const int start = cursor + 1;
        const int end = start + len;
        if (end > packet.size()) {
            return {};
        }
        labels << QString::fromUtf8(packet.mid(start, len));
        cursor = end;
        if (!jumped) {
            next = cursor;
        }
    }
    return {};
}

QList<MdnsDiscovery::Discovered> MdnsDiscovery::parseResponse(const QByteArray &packet, const QHostAddress &fallbackIp) const
{
    QList<Discovered> out;
    if (packet.size() < 12 || (readU16(packet, 2) & 0x8000) == 0) {
        return out;
    }

    const int questions = readU16(packet, 4);
    const int records = readU16(packet, 6) + readU16(packet, 8) + readU16(packet, 10);
    int offset = 12;
    for (int i = 0; i < questions; ++i) {
        readName(packet, offset);
        offset += 4;
    }

    QSet<QString> instances;
    QHash<QString, QPair<QString, quint16>> srvRecords;
    QHash<QString, QHostAddress> aRecords;

    for (int i = 0; i < records && offset + 10 <= packet.size(); ++i) {
        const QString name = readName(packet, offset);
        const quint16 type = readU16(packet, offset);
        const quint16 len = readU16(packet, offset + 8);
        offset += 10;
        const int rdata = offset;
        if (rdata + len > packet.size()) {
            break;
        }

        if (type == TypePtr && name.compare(Service, Qt::CaseInsensitive) == 0) {
            int ptrOffset = rdata;
            const QString instance = readName(packet, ptrOffset);
            if (!instance.isEmpty() && instance.compare(m_instance, Qt::CaseInsensitive) != 0) {
                instances.insert(instance);
            }
        } else if (type == TypeSrv && len >= 7) {
            const quint16 port = readU16(packet, rdata + 4);
            int hostOffset = rdata + 6;
            const QString target = readName(packet, hostOffset);
            if (!target.isEmpty()) {
                srvRecords.insert(name, qMakePair(target, port));
            }
        } else if (type == TypeA && len == 4) {
            const quint32 ip = readU32(packet, rdata);
            aRecords.insert(name, QHostAddress(ip));
        }
        offset = rdata + len;
    }

    for (auto it = srvRecords.constBegin(); it != srvRecords.constEnd(); ++it) {
        if (!instances.isEmpty() && !instances.contains(it.key())) {
            continue;
        }
        if (!it.key().endsWith(Service) || it.value().second == 0) {
            continue;
        }
        Discovered peer;
        peer.hostname = it.key().left(it.key().size() - Service.size()).chopped(1);
        peer.ip = aRecords.value(it.value().first, fallbackIp);
        peer.port = it.value().second;
        if (!peer.ip.isNull()) {
            out << peer;
        }
    }
    return out;
}

bool MdnsDiscovery::isQueryForService(const QByteArray &packet) const
{
    if (packet.size() < 12 || (readU16(packet, 2) & 0x8000) != 0) {
        return false;
    }
    const int questions = readU16(packet, 4);
    int offset = 12;
    for (int i = 0; i < questions; ++i) {
        const QString name = readName(packet, offset);
        if (name.compare(Service, Qt::CaseInsensitive) == 0) {
            return true;
        }
        offset += 4;
    }
    return false;
}

QHostAddress MdnsDiscovery::localIPv4() const
{
    const QList<QHostAddress> addresses = QNetworkInterface::allAddresses();
    for (const QHostAddress &address : addresses) {
        if (address.protocol() == QAbstractSocket::IPv4Protocol && !address.isLoopback()) {
            return address;
        }
    }
    return QHostAddress::LocalHost;
}

quint16 MdnsDiscovery::readU16(const QByteArray &packet, int offset)
{
    if (offset + 1 >= packet.size()) {
        return 0;
    }
    return (static_cast<quint8>(packet.at(offset)) << 8) | static_cast<quint8>(packet.at(offset + 1));
}

quint32 MdnsDiscovery::readU32(const QByteArray &packet, int offset)
{
    if (offset + 3 >= packet.size()) {
        return 0;
    }
    return (static_cast<quint32>(static_cast<quint8>(packet.at(offset))) << 24)
        | (static_cast<quint32>(static_cast<quint8>(packet.at(offset + 1))) << 16)
        | (static_cast<quint32>(static_cast<quint8>(packet.at(offset + 2))) << 8)
        | static_cast<quint8>(packet.at(offset + 3));
}

QString MdnsDiscovery::sanitizeLabel(const QString &label)
{
    QString out;
    for (const QChar c : label) {
        if (c.isLetterOrNumber() || c == '-') {
            out.append(c);
        } else {
            out.append('-');
        }
    }
    return out.left(40).trimmed();
}
