#include "ui/MainWindow.h"
#include "network/NetInterface.h"

#include <QApplication>
#include <QDateTime>
#include <QDir>
#include <QFileDialog>
#include <QFormLayout>
#include <QGroupBox>
#include <QHBoxLayout>
#include <QHeaderView>
#include <QHostAddress>
#include <QMessageBox>
#include <QSpinBox>
#include <QSplitter>
#include <QVBoxLayout>

MainWindow::MainWindow(QWidget *parent)
    : QMainWindow(parent)
{
    buildUi();
    connect(&m_engine, &SyncEngine::stateChanged, this, &MainWindow::refreshState);
    connect(&m_engine, &SyncEngine::logAdded, this, &MainWindow::appendLog);
    refreshState();
}

void MainWindow::buildUi()
{
    auto *central = new QWidget(this);
    auto *root = new QVBoxLayout(central);

    auto *configBox = new QGroupBox(QStringLiteral("配置"), central);
    auto *config = new QGridLayout(configBox);

    m_dirEdit = new QLineEdit(QDir::currentPath(), configBox);
    auto *chooseButton = new QPushButton(QStringLiteral("选择目录"), configBox);
    connect(chooseButton, &QPushButton::clicked, this, &MainWindow::chooseDirectory);

    m_portEdit = new QLineEdit(QStringLiteral("9876"), configBox);
    m_portEdit->setMaximumWidth(100);

    m_startButton = new QPushButton(QStringLiteral("启动"), configBox);
    connect(m_startButton, &QPushButton::clicked, this, &MainWindow::toggleStart);

    config->addWidget(new QLabel(QStringLiteral("同步目录")), 0, 0);
    config->addWidget(m_dirEdit, 0, 1);
    config->addWidget(chooseButton, 0, 2);
    config->addWidget(new QLabel(QStringLiteral("端口")), 0, 3);
    config->addWidget(m_portEdit, 0, 4);
    config->addWidget(m_startButton, 0, 5);

    m_ifaceCombo = new QComboBox(configBox);
    const QList<NetInterfaceEntry> ifaces = NetInterface::availableInterfaces();
    m_ifaceCombo->addItem(QStringLiteral("自动 (所有网卡)"), QString());
    for (const NetInterfaceEntry &entry : ifaces) {
        m_ifaceCombo->addItem(QStringLiteral("%1 - %2").arg(entry.description, entry.address.toString()), entry.address.toString());
    }
    m_ifaceCombo->setMinimumWidth(180);

    config->addWidget(new QLabel(QStringLiteral("网卡")), 1, 0);
    config->addWidget(m_ifaceCombo, 1, 1, 1, 4);

    m_peerEdit = new QLineEdit(configBox);
    m_peerEdit->setPlaceholderText(QStringLiteral("192.168.1.10:9876"));
    auto *connectButton = new QPushButton(QStringLiteral("连接节点"), configBox);
    connect(connectButton, &QPushButton::clicked, this, &MainWindow::connectPeer);
    connect(m_peerEdit, &QLineEdit::returnPressed, this, &MainWindow::connectPeer);

    auto *resendButton = new QPushButton(QStringLiteral("重新发送索引"), configBox);
    connect(resendButton, &QPushButton::clicked, &m_engine, &SyncEngine::resendIndex);

    config->addWidget(new QLabel(QStringLiteral("手动连接")), 2, 0);
    config->addWidget(m_peerEdit, 2, 1, 1, 4);
    config->addWidget(connectButton, 2, 5);
    config->addWidget(resendButton, 2, 6);

    root->addWidget(configBox);

    auto *statusBox = new QGroupBox(QStringLiteral("状态"), central);
    auto *status = new QHBoxLayout(statusBox);
    m_statusLabel = new QLabel(statusBox);
    m_idLabel = new QLabel(statusBox);
    m_countLabel = new QLabel(statusBox);
    status->addWidget(m_statusLabel, 1);
    status->addWidget(m_idLabel, 2);
    status->addWidget(m_countLabel, 2);
    root->addWidget(statusBox);

    auto *splitter = new QSplitter(Qt::Vertical, central);
    m_peerTable = new QTableWidget(splitter);
    m_peerTable->setColumnCount(4);
    m_peerTable->setHorizontalHeaderLabels({QStringLiteral("地址"), QStringLiteral("Peer ID"), QStringLiteral("主机名"), QStringLiteral("状态")});
    m_peerTable->horizontalHeader()->setStretchLastSection(true);
    m_peerTable->horizontalHeader()->setSectionResizeMode(QHeaderView::Stretch);
    m_peerTable->setEditTriggers(QAbstractItemView::NoEditTriggers);
    m_peerTable->setSelectionBehavior(QAbstractItemView::SelectRows);

    m_log = new QPlainTextEdit(splitter);
    m_log->setReadOnly(true);
    m_log->setMaximumBlockCount(500);

    splitter->addWidget(m_peerTable);
    splitter->addWidget(m_log);
    splitter->setStretchFactor(0, 1);
    splitter->setStretchFactor(1, 2);
    root->addWidget(splitter, 1);

    setCentralWidget(central);
    resize(980, 680);
    setWindowTitle(QStringLiteral("LanSync Qt"));
}

void MainWindow::chooseDirectory()
{
    const QString dir = QFileDialog::getExistingDirectory(this, QStringLiteral("选择同步目录"), m_dirEdit->text());
    if (!dir.isEmpty()) {
        m_dirEdit->setText(dir);
    }
}

void MainWindow::toggleStart()
{
    if (m_engine.isRunning()) {
        m_engine.stop();
        refreshState();
        return;
    }

    bool ok = false;
    const quint16 port = m_portEdit->text().toUShort(&ok);
    if (!ok || port == 0) {
        QMessageBox::warning(this, QStringLiteral("端口无效"), QStringLiteral("请输入 1-65535 的端口"));
        return;
    }

    QHostAddress bindAddr(QHostAddress::Any);
    const QString ifaceData = m_ifaceCombo->currentData().toString();
    if (!ifaceData.isEmpty()) {
        bindAddr = QHostAddress(ifaceData);
    }

    if (!m_engine.start(m_dirEdit->text(), port, bindAddr)) {
        QMessageBox::critical(this, QStringLiteral("启动失败"), QStringLiteral("无法启动 TCP 监听，请检查端口是否被占用"));
    }
    refreshState();
}

void MainWindow::connectPeer()
{
    const QString addr = m_peerEdit->text().trimmed();
    if (addr.isEmpty()) {
        return;
    }
    if (!m_engine.isRunning()) {
        QMessageBox::information(this, QStringLiteral("尚未启动"), QStringLiteral("请先启动同步引擎"));
        return;
    }
    m_engine.connectTo(addr);
}

void MainWindow::refreshState()
{
    const bool running = m_engine.isRunning();
    m_startButton->setText(running ? QStringLiteral("停止") : QStringLiteral("启动"));
    m_dirEdit->setEnabled(!running);
    m_portEdit->setEnabled(!running);
    m_ifaceCombo->setEnabled(!running);
    m_statusLabel->setText(running ? QStringLiteral("运行中  TCP %1").arg(m_engine.port()) : QStringLiteral("已停止"));
    m_idLabel->setText(QStringLiteral("ID: %1").arg(m_engine.myId()));
    m_countLabel->setText(QStringLiteral("连接 %1 / 节点 %2 / 同步 %3 / 发送 %4 / 请求 %5")
                              .arg(m_engine.connectedCount())
                              .arg(m_engine.peers().size())
                              .arg(m_engine.syncedFiles())
                              .arg(m_engine.sentFiles())
                              .arg(m_engine.requestedFiles()));

    const QList<PeerEntry> peers = m_engine.peers();
    m_peerTable->setRowCount(peers.size());
    for (int row = 0; row < peers.size(); ++row) {
        const PeerEntry &peer = peers.at(row);
        const QString shortId = peer.peerId.size() > 16 ? peer.peerId.left(16) + "..." : peer.peerId;
        m_peerTable->setItem(row, 0, new QTableWidgetItem(peer.addr));
        m_peerTable->setItem(row, 1, new QTableWidgetItem(shortId));
        m_peerTable->setItem(row, 2, new QTableWidgetItem(peer.hostname));
        m_peerTable->setItem(row, 3, new QTableWidgetItem(peer.status));
    }
}

void MainWindow::appendLog(const QString &message, const QString &level)
{
    const QString time = QDateTime::currentDateTime().toString(QStringLiteral("HH:mm:ss"));
    m_log->appendPlainText(QStringLiteral("%1 [%2] %3").arg(time, level, message));
}
