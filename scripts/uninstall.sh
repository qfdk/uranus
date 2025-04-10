#!/bin/bash

export GIN_MODE=release
APP_NAME=uranus
ServicePath="/etc/systemd/system/${APP_NAME}.service"
INSTALL_PATH="/etc/uranus"

FontGreen="\033[32m"
FontRed="\033[31m"
FontYellow="\033[33m"
FontSuffix="\033[0m"

echo -e "${FontYellow}========== Uranus 卸载程序 ==========${FontSuffix}"

# Check if script is run as root
if [ "$(id -u)" -ne 0 ]; then
    echo -e "${FontRed}错误: 必须以 root 权限运行卸载程序${FontSuffix}"
    echo -e "请使用: sudo bash $0"
    exit 1
fi

# Check if service is installed
if ! systemctl list-unit-files | grep -qw ${APP_NAME}; then
    echo -e "${FontYellow}警告: Uranus 服务未找到${FontSuffix}"
else
    # Stop the service if it's running
    if systemctl is-active --quiet ${APP_NAME}; then
        echo -e "${FontYellow}停止 Uranus 服务...${FontSuffix}"
        systemctl stop ${APP_NAME}
        if [ $? -eq 0 ]; then
            echo -e "${FontGreen}Uranus 服务已停止${FontSuffix}"
        else
            echo -e "${FontRed}停止 Uranus 服务失败${FontSuffix}"
        fi
    fi

    # Disable the service
    if systemctl is-enabled --quiet ${APP_NAME}; then
        echo -e "${FontYellow}禁用 Uranus 服务...${FontSuffix}"
        systemctl disable ${APP_NAME}
        if [ $? -eq 0 ]; then
            echo -e "${FontGreen}Uranus 服务已禁用${FontSuffix}"
        else
            echo -e "${FontRed}禁用 Uranus 服务失败${FontSuffix}"
        fi
    fi

    # Remove service file
    if [ -f ${ServicePath} ]; then
        echo -e "${FontYellow}删除服务文件...${FontSuffix}"
        rm -f ${ServicePath}
        systemctl daemon-reload
        echo -e "${FontGreen}服务文件已删除${FontSuffix}"
    fi
fi

# Remove installation directory
if [ -d ${INSTALL_PATH} ]; then
    echo -e "${FontYellow}删除安装目录 ${INSTALL_PATH}...${FontSuffix}"
    read -p "确认删除安装目录? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf ${INSTALL_PATH}
        echo -e "${FontGreen}安装目录已删除${FontSuffix}"
    else
        echo -e "${FontYellow}保留安装目录${FontSuffix}"
    fi
else
    echo -e "${FontYellow}安装目录 ${INSTALL_PATH} 不存在${FontSuffix}"
fi

# Check for any leftover processes
PIDS=$(pgrep ${APP_NAME})
if [ ! -z "$PIDS" ]; then
    echo -e "${FontYellow}发现 Uranus 进程仍在运行 (PID: $PIDS)${FontSuffix}"
    read -p "是否结束这些进程? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        kill -9 $PIDS 2>/dev/null
        echo -e "${FontGreen}进程已终止${FontSuffix}"
    fi
fi

echo -e "${FontGreen}========== Uranus 卸载完成 ==========${FontSuffix}"