package main

import "time"

const systemScript = `#!/bin/bash
set -Eeuo pipefail
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl git openssh-client xz-utils logrotate \
  build-essential python3-dev libffi-dev \
  nodejs npm postgresql nginx certbot python3-certbot-nginx

SWAP_BYTES=1073741824
if [ -f /swapfile ] && [ "$(stat -c %s /swapfile 2>/dev/null || echo 0)" != "$SWAP_BYTES" ]; then
  swapoff /swapfile 2>/dev/null || true
  rm -f /swapfile
fi
if [ ! -f /swapfile ]; then
  fallocate -l 1G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=1024 status=none
  chmod 600 /swapfile
  mkswap /swapfile >/dev/null
fi
swapon --show=NAME --noheadings | grep -qx /swapfile || swapon /swapfile
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
cat >/etc/sysctl.d/90-cloud-low-memory.conf <<'SYSCTL'
vm.swappiness=10
vm.vfs_cache_pressure=100
SYSCTL
sysctl -p /etc/sysctl.d/90-cloud-low-memory.conf >/dev/null

install -d /etc/systemd/journald.conf.d
cat >/etc/systemd/journald.conf.d/90-cloud-small.conf <<'JOURNAL'
[Journal]
SystemMaxUse=64M
RuntimeMaxUse=64M
SystemKeepFree=256M
MaxRetentionSec=7day
MaxFileSec=1day
Compress=yes
JOURNAL
systemctl restart systemd-journald

cat >/etc/logrotate.d/nginx <<'LOGROTATE'
/var/log/nginx/*.log {
    daily
    rotate 5
    maxsize 5M
    missingok
    notifempty
    compress
    delaycompress
    create 0640 www-data adm
    sharedscripts
    postrotate
        invoke-rc.d nginx rotate >/dev/null 2>&1
    endscript
}
LOGROTATE
cat >/etc/logrotate.d/postgresql-common <<'LOGROTATE'
/var/log/postgresql/*.log {
    daily
    rotate 5
    maxsize 5M
    missingok
    notifempty
    compress
    delaycompress
    copytruncate
}
LOGROTATE
systemctl enable --now logrotate.timer >/dev/null 2>&1 || true

for d in /etc/postgresql/*/main; do
  [ -d "$d" ] || continue
  mkdir -p "$d/conf.d"
  cat >"$d/conf.d/90-cloud-low-memory.conf" <<'PG'
shared_buffers = 64MB
work_mem = 2MB
maintenance_work_mem = 32MB
max_connections = 20
effective_cache_size = 256MB
PG
done
systemctl enable --now postgresql nginx >/dev/null
systemctl restart postgresql

su - postgres -c "psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='root'\"" | grep -q 1 || su - postgres -c "createuser root"
su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='web'\"" | grep -q 1 || su - postgres -c "createdb -O root web"

# Hermes installs the Linux gateway as a systemd user service for root.
# Pre-create its drop-in so a later gateway setup picks up the VM limits.
mkdir -p /root/.config/systemd/user/hermes-gateway.service.d
cat >/root/.config/systemd/user/hermes-gateway.service.d/limits.conf <<'LIMITS'
[Service]
MemoryHigh=360M
MemoryMax=480M
LIMITS

apt-get clean
rm -rf /var/lib/apt/lists/*
`

func setupSystem(cfg config) (commandResult, error) {
	return runRemoteScript(cfg, 15*time.Minute, systemScript)
}
