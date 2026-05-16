#include "filesystem/FileWatcher.h"

#include "filesystem/FileIndexer.h"

#include <QDir>
#include <QDirIterator>
#include <QFileInfo>
#include <QTimer>

namespace {
qint64 modifiedMsecs(const QFileInfo &info)
{
    return info.lastModified().toMSecsSinceEpoch();
}
}

FileWatcher::FileWatcher(QObject *parent)
    : QObject(parent)
{
    connect(&m_watcher, &QFileSystemWatcher::fileChanged, this, &FileWatcher::handleFileChanged);
    connect(&m_watcher, &QFileSystemWatcher::directoryChanged, this, &FileWatcher::handleDirectoryChanged);

    auto *timer = new QTimer(this);
    timer->setInterval(5000);
    connect(timer, &QTimer::timeout, this, &FileWatcher::cleanupIgnored);
    timer->start();
}

bool FileWatcher::start(const QString &root)
{
    stop();
    m_root = QDir(root).absolutePath();
    QDir().mkpath(m_root);
    m_ignored.clear();
    m_knownFiles.clear();
    addRecursive(m_root, true);
    return true;
}

void FileWatcher::stop()
{
    const QStringList files = m_watcher.files();
    if (!files.isEmpty()) {
        m_watcher.removePaths(files);
    }
    const QStringList dirs = m_watcher.directories();
    if (!dirs.isEmpty()) {
        m_watcher.removePaths(dirs);
    }
    m_knownFiles.clear();
}

void FileWatcher::addIgnorePath(const QString &path)
{
    m_ignored.insert(QDir::cleanPath(path), QDateTime::currentDateTimeUtc());
}

QString FileWatcher::root() const
{
    return m_root;
}

void FileWatcher::addRecursive(const QString &path, bool takeSnapshot)
{
    const QFileInfo rootInfo(path);
    if (!rootInfo.exists()) {
        return;
    }

    QStringList toWatch;
    if (rootInfo.isDir()) {
        QDirIterator it(path,
                        QDir::AllEntries | QDir::NoDotAndDotDot | QDir::Hidden | QDir::System,
                        QDirIterator::Subdirectories);
        toWatch << QDir::cleanPath(path);
        while (it.hasNext()) {
            const QString entry = QDir::cleanPath(it.next());
            toWatch << entry;
            if (takeSnapshot) {
                const QFileInfo info(entry);
                if (info.isFile()) {
                    rememberFile(entry, info);
                }
            }
        }
    } else {
        const QString cleanPath = QDir::cleanPath(path);
        toWatch << cleanPath;
        if (takeSnapshot && rootInfo.isFile()) {
            rememberFile(cleanPath, rootInfo);
        }
    }

    for (const QString &entry : toWatch) {
        if (!m_watcher.files().contains(entry) && !m_watcher.directories().contains(entry)) {
            m_watcher.addPath(entry);
        }
    }
}

void FileWatcher::rememberFile(const QString &path, const QFileInfo &info)
{
    m_knownFiles.insert(QDir::cleanPath(path), FileState{info.size(), modifiedMsecs(info)});
}

bool FileWatcher::hasFileChanged(const QString &path, const QFileInfo &info) const
{
    const auto it = m_knownFiles.constFind(QDir::cleanPath(path));
    if (it == m_knownFiles.constEnd()) {
        return true;
    }
    return it->size != info.size() || it->modifiedMsecs != modifiedMsecs(info);
}

void FileWatcher::handleFileChanged(const QString &path)
{
    const QString cleanPath = QDir::cleanPath(path);
    if (shouldIgnore(cleanPath)) {
        const QFileInfo info(cleanPath);
        if (info.exists() && info.isFile()) {
            rememberFile(cleanPath, info);
        }
        return;
    }

    const QFileInfo info(cleanPath);
    if (!info.exists() || !info.isFile()) {
        m_knownFiles.remove(cleanPath);
        return;
    }

    if (!m_watcher.files().contains(cleanPath)) {
        m_watcher.addPath(cleanPath);
    }

    const QString relPath = FileIndexer::toSlashPath(QDir(m_root).relativeFilePath(cleanPath));
    if (!FileIndexer::isPathSafe(relPath)) {
        return;
    }

    if (!hasFileChanged(cleanPath, info)) {
        return;
    }

    const QString hash = FileIndexer::calculateHash(cleanPath);
    if (hash.isEmpty()) {
        return;
    }

    rememberFile(cleanPath, info);
    emit fileChangedMessage(SyncMessage::notify(relPath, hash, info.size(), info.lastModified().toSecsSinceEpoch()));
}

void FileWatcher::handleDirectoryChanged(const QString &path)
{
    addRecursive(path, false);

    QDirIterator it(path,
                    QDir::Files | QDir::NoDotAndDotDot | QDir::Hidden | QDir::System,
                    QDirIterator::Subdirectories);
    while (it.hasNext()) {
        handleFileChanged(it.next());
    }
}

bool FileWatcher::shouldIgnore(const QString &path) const
{
    const auto it = m_ignored.constFind(QDir::cleanPath(path));
    if (it == m_ignored.constEnd()) {
        return false;
    }
    return it.value().msecsTo(QDateTime::currentDateTimeUtc()) < 1000;
}

void FileWatcher::cleanupIgnored()
{
    const QDateTime now = QDateTime::currentDateTimeUtc();
    for (auto it = m_ignored.begin(); it != m_ignored.end();) {
        if (it.value().msecsTo(now) > 5000) {
            it = m_ignored.erase(it);
        } else {
            ++it;
        }
    }
}
