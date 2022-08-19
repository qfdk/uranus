#!/usr/bin/env bash

INSTALL_PATH="/etc/uranus"
PLATFORM=$(dpkg --print-architecture)

service uranus stop
cd ${INSTALL_PATH}
wget https://fr.qfdk.me/uranus/uranus-"${PLATFORM}" -O /etc/uranus/uranus
service uranus start
