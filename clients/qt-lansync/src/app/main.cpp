#include "ui/MainWindow.h"

#include <QApplication>
#include <QString>

#include <iostream>

int main(int argc, char *argv[])
{
    for (int i = 1; i < argc; ++i) {
        if (QString::fromLocal8Bit(argv[i]) == QStringLiteral("--version")) {
            std::cout << "LanSync Qt 0.1.0\n";
            return 0;
        }
    }

    QApplication app(argc, argv);
    QApplication::setApplicationName(QStringLiteral("LanSync Qt"));
    QApplication::setOrganizationName(QStringLiteral("LanSync"));

    MainWindow window;
    window.show();

    return app.exec();
}
