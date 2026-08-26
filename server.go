package main

import (
	"time"
)

const systemScript = `#!/bin/bash
set -Eeuo pipefail
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl git openssh-client rsync xz-utils \
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

npm install -g pm2 --no-audit --no-fund

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

pm2 startup systemd -u root --hp /root >/dev/null 2>&1 || true
mkdir -p /etc/systemd/system/hermes-gateway.service.d
cat >/etc/systemd/system/hermes-gateway.service.d/limits.conf <<'LIMITS'
[Service]
MemoryHigh=360M
MemoryMax=480M
LIMITS
systemctl daemon-reload
apt-get clean
rm -rf /var/lib/apt/lists/*
`

const hermesScript = `#!/bin/bash
set -Eeuo pipefail
export PATH="/root/.local/bin:/usr/local/bin:$PATH"

curl -fsSL https://hermes-agent.nousresearch.com/install.sh \
  | bash -s -- --skip-setup --skip-browser --skip-computer-use --no-skills

HERMES="$(command -v hermes)"
[ -n "$HERMES" ]
ln -sf "$HERMES" /usr/local/bin/hermes
hermes skills opt-out --remove --yes >/dev/null 2>&1 || hermes skills opt-out >/dev/null

hermes config set agent.disabled_toolsets '["browser","computer_use","code_execution","delegation","vision"]' >/dev/null
hermes config set agent.tool_use_enforcement true >/dev/null
hermes config set tool_output.max_bytes 30000 >/dev/null
hermes config set tool_output.max_lines 800 >/dev/null
hermes config set skills.write_approval true >/dev/null
hermes config set memory.memory_enabled true >/dev/null
hermes config set memory.user_profile_enabled true >/dev/null
hermes config set memory.write_approval false >/dev/null

mkdir -p /root/.hermes/skills/farzher-web-development
cat >/root/.hermes/SOUL.md <<'SOUL'
You are Farzher's web developer on a tiny production server.
Be direct, fast, practical, and concise. Prefer the smallest straightforward implementation.
The machine has 1 GB RAM and 1 GB emergency swap. Keep work serial and low-memory; never launch browser or computer-use tooling.
Project-specific rules live in /srv/web/repo/.hermes.md and must be followed for development work.
SOUL

cat >/root/.hermes/skills/farzher-web-development/SKILL.md <<'SKILL'
---
name: farzher-web-development
description: Use for every development task on this server. Fast, direct, low-memory web development with immediate commit, push, and deployment.
platforms: [linux]
---
# Farzher Web Development

Use this skill for every task that changes the website, server application, database-facing application code, Nginx application configuration, or deployment code.

## Goal
Quick iteration and quick turnaround. Implement the requested change directly, keep it simple, deploy it, and respond immediately.

## Environment
- Work in /srv/web/repo.
- This server has 1 GB RAM and 1 GB emergency swap. Swap is not working memory; avoid memory pressure.
- Stack: Node.js, PostgreSQL, PM2, Nginx.
- Keep commands serial. Avoid heavyweight dependencies, frameworks, build pipelines, containers, browsers, subagents, and background analysis unless the user specifically asks.

## Development
- Inspect only what is needed, then edit directly.
- Prefer existing patterns and the smallest implementation that solves the request.
- Do not add tests unless explicitly requested.
- Do not run tests, linters, type checks, benchmarks, broad validation, or HTTP health checks unless explicitly requested.
- Do not spend time refactoring unrelated code.

## Finish every code change
1. git add -A
2. git commit -m "<short useful message>"
3. git push
4. /usr/local/bin/deploy-web
5. Reply as soon as the deploy command returns.

If a required command fails, report the failure instead of claiming deployment succeeded. Do not wait for post-deploy health checks.

You may learn new reusable skills with skill_manage; skill writes are approval-gated. Never rewrite this skill, SOUL.md, or .hermes.md unless the user explicitly asks to change the permanent operating rules.
SKILL

hermes --version
`

func setupSystem(cfg config) (commandResult, error) {
	return runRemoteScript(cfg, 15*time.Minute, systemScript)
}

func installHermes(cfg config) (commandResult, error) {
	return runRemoteScript(cfg, 15*time.Minute, hermesScript)
}
