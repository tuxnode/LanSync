cmake_minimum_required(VERSION 3.20)

project(QtLanSync VERSION 0.1.0 LANGUAGES CXX)

if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
    set(CMAKE_BUILD_TYPE "Release" CACHE STRING "Build type" FORCE)
    set_property(CACHE CMAKE_BUILD_TYPE PROPERTY STRINGS "Debug" "Release" "MinSizeRel" "RelWithDebInfo")
endif()

set(CMAKE_CXX_FLAGS_DEBUG "-g -O0")
set(CMAKE_CXX_FLAGS_RELEASE "-O3 -DNDEBUG")
set(CMAKE_CXX_FLAGS_RELWITHDEBINFO "-O2 -g -DNDEBUG")
set(CMAKE_CXX_FLAGS_MINSIZEREL "-Os -DNDEBUG")

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)

set(LANSYNC_QT_MAJOR "AUTO" CACHE STRING "Qt major version: AUTO, 5, or 6")
set_property(CACHE LANSYNC_QT_MAJOR PROPERTY STRINGS AUTO 5 6)

if(LANSYNC_QT_MAJOR STREQUAL "6")
    find_package(Qt6 REQUIRED COMPONENTS Widgets Network)
    set(QT_PACKAGE Qt6)
elseif(LANSYNC_QT_MAJOR STREQUAL "5")
    find_package(Qt5 REQUIRED COMPONENTS Widgets Network)
    set(QT_PACKAGE Qt5)
else()
    find_package(Qt6 QUIET COMPONENTS Widgets Network)
    if(Qt6_FOUND)
        set(QT_PACKAGE Qt6)
    else()
        find_package(Qt5 REQUIRED COMPONENTS Widgets Network)
        set(QT_PACKAGE Qt5)
    endif()
endif()

message(STATUS "Building with ${QT_PACKAGE}")

add_executable(qt-lansync
    src/app/main.cpp
    src/core/SyncEngine.cpp
    src/core/SyncEngine.h
    src/filesystem/FileIndexer.cpp
    src/filesystem/FileIndexer.h
    src/filesystem/FileWatcher.cpp
    src/filesystem/FileWatcher.h
    src/network/MdnsDiscovery.cpp
    src/network/MdnsDiscovery.h
    src/network/NetInterface.h
    src/network/Transport.cpp
    src/network/Transport.h
    src/protocol/Protocol.cpp
    src/protocol/Protocol.h
    src/ui/MainWindow.cpp
    src/ui/MainWindow.h
)

target_include_directories(qt-lansync PRIVATE
    src
)

target_link_libraries(qt-lansync PRIVATE
    ${QT_PACKAGE}::Widgets
    ${QT_PACKAGE}::Network
)

if(WIN32)
    target_compile_definitions(qt-lansync PRIVATE NOMINMAX WIN32_LEAN_AND_MEAN)
    set_target_properties(qt-lansync PROPERTIES WIN32_EXECUTABLE TRUE)
endif()
