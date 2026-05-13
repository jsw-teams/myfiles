#!/usr/bin/env bash
set -euo pipefail

if [ "${PURGE_OLD:-}" != "1" ]; then
  echo "Refusing to purge old mypicture without PURGE_OLD=1"
  echo "Run: sudo PURGE_OLD=1 bash scripts/purge-old-mypicture.sh"
  exit 1
fi

systemctl stop mypicture.service 2>/dev/null || true
systemctl disable mypicture.service 2>/dev/null || true
rm -f /etc/systemd/system/mypicture.service
systemctl daemon-reload

rm -rf /opt/mypicture
rm -rf /var/lib/mypicture
rm -rf /tmp/mypicture*
find /opt -maxdepth 1 -type f \( -name '*mypicture*.zip' -o -name '*mypicture*.tar.gz' -o -name '*mypicture*.bak' \) -delete
find /opt -maxdepth 1 -type d \( -name '*mypicture*.bak' -o -name '*mypicture*-backup*' \) -exec rm -rf {} +

echo "old mypicture files purged"
