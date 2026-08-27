package main

func buildDeployScript() string {
	return `#!/bin/bash
set -Eeuo pipefail
APP=/website/app
DATA=/website/data
STATE=/var/lib/website
exec 9>/run/lock/web-state.lock
flock -n 9 || { echo 'Another deploy, backup, or restore is already running.' >&2; exit 1; }
install -d -m 0750 "$DATA"
install -d -m 0755 "$STATE"
cd "$APP"
CURRENT="$(cat "$STATE/current-slot" 2>/dev/null || true)"
if [ "$CURRENT" = blue ]; then NEXT=green; PORT=3002; else NEXT=blue; PORT=3001; fi
NAME="web-$NEXT"
export npm_config_audit=false npm_config_fund=false npm_config_jobs=1
export NODE_OPTIONS=--max-old-space-size=224
pkg_hash() { { cat package.json; [ ! -f package-lock.json ] || cat package-lock.json; } | sha256sum | awk '{print $1}'; }
HASH="$(pkg_hash)"
if [ ! -d node_modules ] || [ "$(cat "$STATE/deps-hash" 2>/dev/null || true)" != "$HASH" ]; then
  if [ -f package-lock.json ]; then
    npm ci --omit=dev --no-audit --no-fund
  else
    npm install --omit=dev --no-audit --no-fund --no-package-lock
  fi
  HASH="$(pkg_hash)"
  printf '%s\n' "$HASH" >"$STATE/deps-hash"
fi

# Start the inactive slot first. The live Nginx upstream is untouched until
# the new process proves it can answer a successful local HTTP request.
pm2 delete "$NAME" >/dev/null 2>&1 || true
PORT="$PORT" DATA_DIR="$DATA" DATABASE_URL='postgresql:///web?host=/var/run/postgresql' NODE_OPTIONS=--max-old-space-size=224 \
  pm2 start npm --name "$NAME" --max-memory-restart 300M -- start >/dev/null
READY=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null --max-time 1 "http://127.0.0.1:$PORT/"; then READY=1; break; fi
  sleep 0.1
done
if [ "$READY" != 1 ]; then
  pm2 delete "$NAME" >/dev/null 2>&1 || true
  echo "New $NEXT slot did not become ready; old slot remains live." >&2
  exit 1
fi

OLD_UPSTREAM="$STATE/web-upstream.previous"
if [ -f /etc/nginx/conf.d/web-upstream.conf ]; then
  cp /etc/nginx/conf.d/web-upstream.conf "$OLD_UPSTREAM"
else
  rm -f "$OLD_UPSTREAM"
fi
cat > /etc/nginx/conf.d/web-upstream.conf.tmp <<UPSTREAM
upstream web_backend {
    server 127.0.0.1:$PORT;
}
UPSTREAM
mv /etc/nginx/conf.d/web-upstream.conf.tmp /etc/nginx/conf.d/web-upstream.conf
rollback_nginx() {
  if [ -f "$OLD_UPSTREAM" ]; then cp "$OLD_UPSTREAM" /etc/nginx/conf.d/web-upstream.conf; else rm -f /etc/nginx/conf.d/web-upstream.conf; fi
  systemctl reload nginx >/dev/null 2>&1 || true
  pm2 delete "$NAME" >/dev/null 2>&1 || true
}
if ! nginx -t >/dev/null 2>&1; then
  rollback_nginx
  echo 'Nginx rejected the new upstream; old slot remains live.' >&2
  exit 1
fi
if ! systemctl reload nginx; then
  rollback_nginx
  echo 'Nginx reload failed; old slot remains live.' >&2
  exit 1
fi
if [ -n "$CURRENT" ] && [ "$CURRENT" != "$NEXT" ]; then pm2 delete "web-$CURRENT" >/dev/null 2>&1 || true; fi
printf '%s\n' "$NEXT" >"$STATE/current-slot"
pm2 save --force >/dev/null
rm -f "$OLD_UPSTREAM"
printf 'Deployed %s on port %s.\n' "$NEXT" "$PORT"
`
}
