#!/bin/bash
export GIN_MODE=release
APP_NAME=nginx-proxy-manager
if [ ! -d ~/nginx-proxy-manager  ];then
  mkdir -p ~/nginx-proxy-manager
else
  kill -9 $(ps aux|grep "nginx-proxy-manager"|grep -v grep|awk '{print $2}')
  rm ~/nginx-proxy-manager/$APP_NAME
fi
cd ~/nginx-proxy-manager
wget https://fr.qfdk.me/nginx-proxy-manager
chmod +x $APP_NAME
./$APP_NAME >app.log 2>&1 &