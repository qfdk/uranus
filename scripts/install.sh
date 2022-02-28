#!/bin/bash
export GIN_MODE=release
wget https://fr.qfdk.me/nginx-proxy-manager
chmod +x nginx-proxy-manager
./nginx-proxy-manager >app.log 2>&1 &