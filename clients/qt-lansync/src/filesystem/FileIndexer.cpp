#include "filesystem/FileIndexer.h"

#include <QCryptographicHash>
#include <QDateTime>
#include <QDir>
#include <QDirIterator>
#include <QFile>
#include <QFileInfo>

namespace {
qint64 modifiedSecs(const QFileInfo &info)
{
    return info.lastModified().toSecsSinceEpoch();
}
}

namespace FileIndexer {

bool isPathSafe(const QString &relPath)
{
    QString normalized = relPath;
    normalized.replace('\\', '/');
    if (normalized.isEmpty() || normalized.startsWith('/') || QDir::isAbsolutePath(normalized)) {
        return false;
    }

    const QStringList parts = normalized.split('/', Qt::SkipEmptyParts);
    for (const QString &part : parts) {
        if (part == ".." || part.contains(':')) {
            return false;
        }
    }
    return true;
}

QString toSlashPath(const QString &path)
{
    QString out = QDir::fromNativeSeparators(path);
    while (out.startsWith("./")) {
        out.remove(0, 2);
    }
    return out;
}

QString calculateHash(const QString &path)
{
    QFile file(path);
    if (!file.open(QIODevice::ReadOnly)) {
        return {};
    }

    QCryptographicHash hash(QCryptographicHash::Sha256);
    while (!file.atEnd()) {
        hash.addData(file.read(64 * 1024));
    }
    return QString::fromLatin1(hash.result().toHex());
}

IndexMap generateIndex(const QString &root)
{
    IndexMap index;
    const QDir rootDir(root);
    QDirIterator it(root,
                    QDir::AllEntries | QDir::NoDotAndDotDot | QDir::Hidden | QDir::System,
                    QDirIterator::Subdirectories);

    while (it.hasNext()) {
        const QString path = it.next();
        const QFileInfo info(path);
        QString relPath = toSlashPath(rootDir.relativeFilePath(path));
        if (!isPathSafe(relPath)) {
            continue;
        }

        FileInfo fileInfo;
        fileInfo.relPath = relPath;
        fileInfo.size = info.size();
        fileInfo.modTime = modifiedSecs(info);
        fileInfo.isFolder = info.isDir();
        if (!fileInfo.isFolder) {
            fileInfo.hash = calculateHash(path);
            if (fileInfo.hash.isEmpty()) {
                continue;
            }
        }
        index.insert(relPath, fileInfo);
    }

    return index;
}

QString safeJoin(const QString &root, const QString &relPath)
{
    if (!isPathSafe(relPath)) {
        return {};
    }
    return QDir::cleanPath(QDir(root).filePath(relPath));
}

}
