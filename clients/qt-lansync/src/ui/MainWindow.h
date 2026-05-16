#pragma once

#include "core/SyncEngine.h"

#include <QLabel>
#include <QLineEdit>
#include <QMainWindow>
#include <QPlainTextEdit>
#include <QPushButton>
#include <QTableWidget>

class MainWindow : public QMainWindow {
    Q_OBJECT

public:
    explicit MainWindow(QWidget *parent = nullptr);

private:
    void buildUi();
    void chooseDirectory();
    void toggleStart();
    void connectPeer();
    void refreshState();
    void appendLog(const QString &message, const QString &level);

    SyncEngine m_engine;
    QLineEdit *m_dirEdit = nullptr;
    QLineEdit *m_portEdit = nullptr;
    QLineEdit *m_peerEdit = nullptr;
    QPushButton *m_startButton = nullptr;
    QLabel *m_idLabel = nullptr;
    QLabel *m_statusLabel = nullptr;
    QLabel *m_countLabel = nullptr;
    QTableWidget *m_peerTable = nullptr;
    QPlainTextEdit *m_log = nullptr;
};
