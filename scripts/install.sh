#!/bin/bash
export GIN_MODE=release
APP_NAME=nginx-proxy-manager
ServicePath="/etc/systemd/system/${APP_NAME}.service"
INSTALL_PATH="/etc/nginx-proxy-manager"

FontGreen="\033[32m"
FontRed="\033[31m"
FontYellow="\033[33m"
FontSuffix="\033[0m"

install_service() {
  cat >"$ServicePath" <<EOF
[Unit]
Description=Nginx proxy manager
After=network.target
[Service]
Type=simple
WorkingDirectory=/etc/nginx-proxy-manager
ExecStart=/etc/nginx-proxy-manager/nginx-proxy-manager
User=root
Environment="GIN_MODE=release"
TimeoutStopSec=5
KillMode=mixed
Restart=always
[Install]
WantedBy=multi-user.target
EOF
  chmod 644 "$ServicePath"
  echo "info: Systemd service files have been installed successfully!"
  systemctl daemon-reload
  SYSTEMD='1'
}

start_service() {
  if [[ -f ServicePath ]]; then
    systemctl start ${APP_NAME}
    sleep 1s
    if systemctl -q is-active ${APP_NAME}; then
      echo 'info: Start the Nginx proxy manager service.'
    else
      echo -e "${FontRed}error: Failed to start the Nginx proxy manager service.${FontSuffix}"
      exit 1
    fi
  fi
}

stop_service() {
  if ! systemctl stop ${APP_NAME}; then
    echo -e "${FontRed}error: Failed to stop Nginx proxy manager service.${FontSuffix}"
    exit 1
  fi
  echo "info: Nginx proxy manager service Stopped."
}

main() {

  if systemctl list-unit-files | grep -qw ${APP_NAME}; then
    if [[ -n "$(pidof ${APP_NAME})" ]]; then
      stop_service
      NGINX_PROXY_MANAGER='1'
    fi
  fi

  if [ ! -d ${INSTALL_PATH} ]; then
    mkdir -p ${INSTALL_PATH}
  else
    kill -9 $(pidof ${APP_NAME})
    rm ${INSTALL_PATH}/${APP_NAME}
  fi

  cd ${INSTALL_PATH}
  wget https://fr.qfdk.me/nginx-proxy-manager
  chmod +x $APP_NAME

  install_service
  if [[ "$SYSTEMD" -eq '1' ]]; then
    echo "installed: ${ServicePath}"
  fi

  if [[ "$NGINX_PROXY_MANAGER" -eq '1' ]]; then
    systemctl start ${APP_NAME}
  else
    systemctl start ${APP_NAME}
    systemctl enable ${APP_NAME}
    sleep 1s

    if systemctl -q is-active ${APP_NAME}; then
      echo "info: Start and enable the Nginx proxy manager service."
    else
      echo -e "${FontYellow}warning: Failed to enable and start the Nginx proxy manager service.${FontSuffix}"
    fi
  fi
  #  ./$APP_NAME >app.log 2>&1 &
  install_service
}

main
