#!/usr/bin/env bash

INSTALL_PATH="/etc/uranus"
PLATFORM=$(dpkg --print-architecture)
DOWNLOAD_URL="https://fr.qfdk.me/uranus/uranus-${PLATFORM}"
TEMP_FILE="${INSTALL_PATH}/uranus_new"
OLD_FILE="${INSTALL_PATH}/uranus_old"
CUR_FILE="${INSTALL_PATH}/uranus"

# Check if the service is running and stop it
if service uranus status > /dev/null 2>&1; then
  echo "Stopping Uranus service..."
  service uranus stop
  if [ $? -ne 0 ]; then
    echo "Failed to stop Uranus service. Aborting."
    exit 1
  fi
fi

# Download the new version
echo "Downloading new version of Uranus service..."
wget -q --spider ${DOWNLOAD_URL}
if [ $? -eq 0 ]; then
  wget ${DOWNLOAD_URL} -O ${TEMP_FILE}
  if [ $? -ne 0 ]; then
    echo "Failed to download the new version. Aborting."
    service uranus start
    exit 1
  fi
else
  echo "Unable to reach ${DOWNLOAD_URL}. Aborting."
  service uranus start
  exit 1
fi

# Make the new file executable
chmod +x ${TEMP_FILE}

# Rename the current version for backup
mv ${CUR_FILE} ${OLD_FILE}

# Replace current version with new version
mv ${TEMP_FILE} ${CUR_FILE}

# Start the service again
echo "Starting Uranus service..."
service uranus start
if [ $? -ne 0 ]; then
  echo "Failed to start the Uranus service with the new version. Rolling back..."
  mv ${OLD_FILE} ${CUR_FILE}
  service uranus start
  if [ $? -ne 0 ]; then
    echo "Failed to start the Uranus service with the old version. Manual intervention required."
    exit 1
  fi
  exit 1
fi

# Remove the old file
rm ${OLD_FILE}

echo "Uranus service upgraded successfully."
