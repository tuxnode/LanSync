#pragma once

#include <QJsonObject>
#include <QString>

enum class MessageType : int {
    Notify = 0,
    PullRequest = 1,
    FileData = 2,
    Error = 3,
    HandShake = 4,
    HandShakeReject = 5,
    Bye = 6,
};

struct SyncMessage {
    int type = 0;
    QString relPath;
    QString hash;
    qint64 size = 0;
    qint64 modTime = 0;
    QString peerId;
    qint64 timestamp = 0;
    QString data;

    MessageType kind() const;
    QJsonObject toJson() const;
    static SyncMessage fromJson(const QJsonObject &object);
    static SyncMessage make(MessageType type);
    static SyncMessage notify(const QString &relPath, const QString &hash, qint64 size, qint64 modTime);
    static SyncMessage pullRequest(const QString &relPath);
    static SyncMessage fileData(const QString &relPath, qint64 size, const QString &data);
    static SyncMessage error(const QString &relPath, const QString &data);
    static SyncMessage handshake(const QString &peerId, qint64 timestamp);
};
