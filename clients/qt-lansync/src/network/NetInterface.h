#pragma once

#include <QHostAddress>
#include <QList>
#include <QNetworkInterface>

struct NetInterfaceEntry {
    QString name;
    QString description;
    QHostAddress address;
};

namespace NetInterface {

inline QList<NetInterfaceEntry> availableInterfaces()
{
    QList<NetInterfaceEntry> out;
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
            NetInterfaceEntry e;
            e.address = ip;
            e.name = iface.name();
            if (iface.humanReadableName() != iface.name()) {
                e.description = iface.humanReadableName();
            } else {
                e.description = iface.name();
            }
            out.append(e);
        }
    }
    return out;
}

} // namespace NetInterface
