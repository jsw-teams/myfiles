#!/usr/bin/env bash
set -euo pipefail

sudo install -d -o root -g root /opt/myfiles
sudo rsync -a --delete ./ /opt/myfiles/ --exclude data --exclude frontend/node_modules --exclude .git
sudo install -d -o myfiles -g myfiles /var/lib/myfiles /var/lib/myfiles/tmp /var/lib/myfiles/storage
sudo install -d -o root -g root /etc/myfiles
if [ ! -f /etc/myfiles/config.json ]; then
  sudo cp /opt/myfiles/configs/config.example.json /etc/myfiles/config.json
  echo "created /etc/myfiles/config.json; edit it before starting service"
fi
sudo cp /opt/myfiles/deploy/systemd/myfiles.service /etc/systemd/system/myfiles.service
sudo systemctl daemon-reload
sudo systemctl enable myfiles.service
