#pragma once

#include "protocol/Protocol.h"

#include <QDateTime>
#include <QFileSystemWatcher>
#include <QHash>
#include <QObject>
#include <QString>

class QFileInfo;

class FileWatcher : public QObject {
    Q_OBJECT

public:
    explicit FileWatcher(QObject *parent = nullptr);

    bool start(const QString &root);
    void stop();
    void addIgnorePath(const QString &path);
    QString root() const;

signals:
    void fileChangedMessage(const SyncMessage &message);
    void watcherLog(const QString &message, const QString &level);

private:
    struct FileState {
        qint64 size = -1;
        qint64 modifiedMsecs = -1;
    };

    void addRecursive(const QString &path, bool takeSnapshot);
    void rememberFile(const QString &path, const QFileInfo &info);
    bool hasFileChanged(const QString &path, const QFileInfo &info) const;
    void handleFileChanged(const QString &path);
    void handleDirectoryChanged(const QString &path);
    bool shouldIgnore(const QString &path) const;
    void cleanupIgnored();

    QFileSystemWatcher m_watcher;
    QString m_root;
    QHash<QString, QDateTime> m_ignored;
    QHash<QString, FileState> m_knownFiles;
};
