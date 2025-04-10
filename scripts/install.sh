#!/bin/bash
set -e

export GIN_MODE=release
APP_NAME=uranus
ServicePath="/etc/systemd/system/${APP_NAME}.service"
INSTALL_PATH="/etc/uranus"
PLATFORM=$(dpkg --print-architecture);

FontGreen="\033[32m"
FontRed="\033[31m"
FontYellow="\033[33m"
FontSuffix="\033[0m"

install_service() {
  cat >"$ServicePath" <<EOF
[Unit]
Description=Uranus - A Nginx manager UI
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/etc/uranus
ExecStart=/etc/uranus/uranus
ExecReload=/bin/kill -HUP \$MAINPID
PIDFile=/etc/uranus/uranus.pid
Environment="GIN_MODE=release"

# 平滑升级配置
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
KillMode=mixed
KillSignal=SIGTERM

# 资源限制
LimitNOFILE=65536
LimitNPROC=65536

# 状态和安全配置
StandardOutput=journal
StandardError=journal
SyslogIdentifier=uranus

[Install]
WantedBy=multi-user.target
EOF
  chmod 644 "$ServicePath"
  echo -e "${FontGreen}info: Systemd service files have been installed successfully!${FontSuffix}"
  systemctl daemon-reload
  SYSTEMD='1'
}

start_service() {
  if [[ -f ${ServicePath} ]]; then
    systemctl start ${APP_NAME} || (echo -e "${FontRed}error: Failed to start the Uranus service.${FontSuffix}" && exit 1)
    sleep 1s
    if ! systemctl -q is-active ${APP_NAME}; then
      echo -e "${FontRed}error: Failed to start the Uranus service.${FontSuffix}"
      exit 1
    fi
    echo -e "${FontGreen}info: Start the Uranus service.${FontSuffix}"
  fi
}

stop_service() {
  if systemctl is-active --quiet ${APP_NAME}; then
    systemctl stop ${APP_NAME} || (echo -e "${FontRed}error: Failed to stop Uranus service.${FontSuffix}" && exit 1)
    echo -e "${FontGreen}info: Uranus service Stopped.${FontSuffix}"
  fi
}

create_directories() {
  # 创建主安装目录
  if [ ! -d ${INSTALL_PATH} ]; then
    mkdir -p ${INSTALL_PATH}
    echo -e "${FontGreen}info: Created installation directory ${INSTALL_PATH}${FontSuffix}"
  fi

  # 创建日志目录
  if [ ! -d ${INSTALL_PATH}/logs ]; then
    mkdir -p ${INSTALL_PATH}/logs
    echo -e "${FontGreen}info: Created logs directory ${INSTALL_PATH}/logs${FontSuffix}"
  fi

  # 确保目录权限正确
  chmod 755 ${INSTALL_PATH}
  chmod 755 ${INSTALL_PATH}/logs
}

main() {
  echo -e "${FontGreen}========== Uranus 安装程序 ==========${FontSuffix}"
  echo -e "${FontGreen}平台: ${PLATFORM}${FontSuffix}"
  echo -e "${FontGreen}安装路径: ${INSTALL_PATH}${FontSuffix}"

  # 检查是否已安装并运行
  if systemctl list-unit-files | grep -qw ${APP_NAME}; then
    if [[ -n "$(pidof ${APP_NAME})" ]]; then
      echo -e "${FontYellow}检测到已安装的服务正在运行${FontSuffix}"
      stop_service
      URANUS='1'
    fi
  fi

  # 创建必要的目录
  create_directories

  # 备份旧的可执行文件（如果存在）
  if [ -f ${INSTALL_PATH}/${APP_NAME} ]; then
    echo -e "${FontYellow}备份当前可执行文件...${FontSuffix}"
    mv ${INSTALL_PATH}/${APP_NAME} ${INSTALL_PATH}/${APP_NAME}.bak.$(date +"%Y%m%d%H%M%S")
  fi

  # 下载新的可执行文件
  echo -e "${FontGreen}正在下载 Uranus...${FontSuffix}"
  wget -q --show-progress https://fr.qfdk.me/uranus/uranus-"${PLATFORM}" -O ${INSTALL_PATH}/uranus
  chmod +x ${INSTALL_PATH}/${APP_NAME}
  echo -e "${FontGreen}下载完成并设置执行权限${FontSuffix}"

  # 安装服务
  echo -e "${FontGreen}安装 systemd 服务...${FontSuffix}"
  install_service
  if [[ "$SYSTEMD" -eq '1' ]]; then
    echo -e "${FontGreen}已安装: ${ServicePath}${FontSuffix}"
  fi

  # 启动服务
  if [[ "$URANUS" -eq '1' ]]; then
    start_service
  else
    systemctl enable ${APP_NAME}
    start_service
    if ! systemctl is-enabled --quiet ${APP_NAME}; then
      echo -e "${FontYellow}warning: Failed to enable the Uranus service.${FontSuffix}"
    fi
    echo -e "${FontGreen}info: 已启动并启用 Uranus 服务。${FontSuffix}"
  fi

  echo -e "${FontGreen}========== 安装完成 ==========${FontSuffix}"
  echo -e "${FontGreen}您可以使用以下命令管理服务:${FontSuffix}"
  echo -e "${FontGreen}  启动: systemctl start ${APP_NAME}${FontSuffix}"
  echo -e "${FontGreen}  停止: systemctl stop ${APP_NAME}${FontSuffix}"
  echo -e "${FontGreen}  重启: systemctl restart ${APP_NAME}${FontSuffix}"
  echo -e "${FontGreen}  状态: systemctl status ${APP_NAME}${FontSuffix}"
}

main
