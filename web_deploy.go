package main

func buildDeployScript() string {
	return `#!/bin/bash
set -Eeuo pipefail
APP=/website/app
STATE=/var/lib/website
exec 9>/run/lock/web-state.lock
flock -n 9 || { echo 'Another deploy, backup, or restore is already running.' >&2; exit 1; }
install -d -m 0755 "$STATE"
cd "$APP"
export npm_config_audit=false npm_config_fund=false npm_config_jobs=1
pkg_hash() { { cat package.json; [ ! -f package-lock.json ] || cat package-lock.json; } | sha256sum | awk '{print $1}'; }
HASH="$(pkg_hash)"
if [ ! -d node_modules ] || [ "$(cat "$STATE/deps-hash" 2>/dev/null || true)" != "$HASH" ]; then
  if [ -f package-lock.json ]; then
    npm ci --omit=dev --no-audit --no-fund
  else
    npm install --omit=dev --no-audit --no-fund --no-package-lock
  fi
  printf '%s\n' "$(pkg_hash)" >"$STATE/deps-hash"
fi

systemctl restart web
READY=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null --max-time 1 http://127.0.0.1:3000/; then READY=1; break; fi
  sleep 0.1
done
if [ "$READY" != 1 ]; then
  systemctl status web --no-pager >&2 || true
  echo 'Web service did not become ready.' >&2
  exit 1
fi
printf 'Deployed on port 3000.\n'
`
}
