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
ExecStart=/etc/uranus/uranus
ExecReload=/bin/kill -HUP '$MAINPID'
PIDFile=/etc/uranus/uranus.pid
Environment="GIN_MODE=release"
[Install]
WantedBy=multi-user.target
EOF
  chmod 644 "$ServicePath"
  echo "info: Systemd service files have been installed successfully!"
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
    echo 'info: Start the Uranus service.'
  fi
}

stop_service() {
  if systemctl is-active --quiet ${APP_NAME}; then
    systemctl stop ${APP_NAME} || (echo -e "${FontRed}error: Failed to stop Uranus service.${FontSuffix}" && exit 1)
    echo "info: Uranus service Stopped."
  fi
}

main() {

  if systemctl list-unit-files | grep -qw ${APP_NAME}; then
    if [[ -n "$(pidof ${APP_NAME})" ]]; then
      stop_service
      URANUS='1'
    fi
  fi

  if [ ! -d ${INSTALL_PATH} ]; then
    mkdir -p ${INSTALL_PATH}
  else
    if systemctl is-active --quiet ${APP_NAME}; then
      stop_service
    fi
    rm -f ${INSTALL_PATH}/${APP_NAME}
  fi

  if [ ! -d ${INSTALL_PATH}/logs ]; then
    mkdir -p ${INSTALL_PATH}/logs
  fi

  cd ${INSTALL_PATH}
  wget https://fr.qfdk.me/uranus/uranus-"${PLATFORM}" -O /etc/uranus/uranus

  chmod +x $APP_NAME

  install_service
  if [[ "$SYSTEMD" -eq '1' ]]; then
    echo "installed: ${ServicePath}"
  fi

  if [[ "$URANUS" -eq '1' ]]; then
    start_service
  else
    systemctl enable ${APP_NAME}
    start_service
    if ! systemctl is-enabled --quiet ${APP_NAME}; then
      echo -e "${FontYellow}warning: Failed to enable the Uranus service.${FontSuffix}"
    fi
    echo "info: Start and enable the Uranus service."
  fi
}

main
