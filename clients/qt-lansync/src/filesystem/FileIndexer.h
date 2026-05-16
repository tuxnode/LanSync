#pragma once

#include <QHash>
#include <QString>

struct FileInfo {
    QString relPath;
    QString hash;
    qint64 size = 0;
    qint64 modTime = 0;
    bool isFolder = false;
};

using IndexMap = QHash<QString, FileInfo>;

namespace FileIndexer {
bool isPathSafe(const QString &relPath);
QString toSlashPath(const QString &path);
QString calculateHash(const QString &path);
IndexMap generateIndex(const QString &root);
QString safeJoin(const QString &root, const QString &relPath);
}
