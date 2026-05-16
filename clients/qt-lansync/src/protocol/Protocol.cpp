#include "protocol/Protocol.h"

MessageType SyncMessage::kind() const
{
    return static_cast<MessageType>(type);
}

QJsonObject SyncMessage::toJson() const
{
    QJsonObject object;
    object["type"] = type;
    if (!relPath.isEmpty()) {
        object["rel_path"] = relPath;
    }
    if (!hash.isEmpty()) {
        object["hash"] = hash;
    }
    if (size != 0) {
        object["size"] = static_cast<double>(size);
    }
    if (modTime != 0) {
        object["mod_time"] = static_cast<double>(modTime);
    }
    if (!peerId.isEmpty()) {
        object["peer_id"] = peerId;
    }
    if (timestamp != 0) {
        object["timestamp"] = static_cast<double>(timestamp);
    }
    if (!data.isEmpty()) {
        object["data"] = data;
    }
    return object;
}

SyncMessage SyncMessage::fromJson(const QJsonObject &object)
{
    SyncMessage message;
    message.type = object.value("type").toInt();
    message.relPath = object.value("rel_path").toString();
    message.hash = object.value("hash").toString();
    message.size = static_cast<qint64>(object.value("size").toDouble());
    message.modTime = static_cast<qint64>(object.value("mod_time").toDouble());
    message.peerId = object.value("peer_id").toString();
    message.timestamp = static_cast<qint64>(object.value("timestamp").toDouble());
    message.data = object.value("data").toString();
    return message;
}

SyncMessage SyncMessage::make(MessageType type)
{
    SyncMessage message;
    message.type = static_cast<int>(type);
    return message;
}

SyncMessage SyncMessage::notify(const QString &relPath, const QString &hash, qint64 size, qint64 modTime)
{
    SyncMessage message = make(MessageType::Notify);
    message.relPath = relPath;
    message.hash = hash;
    message.size = size;
    message.modTime = modTime;
    return message;
}

SyncMessage SyncMessage::pullRequest(const QString &relPath)
{
    SyncMessage message = make(MessageType::PullRequest);
    message.relPath = relPath;
    return message;
}

SyncMessage SyncMessage::fileData(const QString &relPath, qint64 size, const QString &data)
{
    SyncMessage message = make(MessageType::FileData);
    message.relPath = relPath;
    message.size = size;
    message.data = data;
    return message;
}

SyncMessage SyncMessage::error(const QString &relPath, const QString &data)
{
    SyncMessage message = make(MessageType::Error);
    message.relPath = relPath;
    message.data = data;
    return message;
}

SyncMessage SyncMessage::handshake(const QString &peerId, qint64 timestamp)
{
    SyncMessage message = make(MessageType::HandShake);
    message.peerId = peerId;
    message.timestamp = timestamp;
    return message;
}
