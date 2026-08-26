package main

func buildWebProvisionScript(cfg config, serverName, hermesContext string) string {
	return `set -Eeuo pipefail
REPO=/website
DATA="$REPO/data"
STATE=/var/lib/website
install -d -m 0755 "$STATE"
mkdir -p "$REPO/ops"

# One-time migration from the older /srv/web layout. A running site's slot
# marker makes it an existing server, so migration never triggers an automatic
# restore over live state.
for NAME in current-slot deps-hash state-initialized; do
  if [ -e "/srv/web/$NAME" ] && [ ! -e "$STATE/$NAME" ]; then mv "/srv/web/$NAME" "$STATE/$NAME"; fi
done
install -d -m 0750 "$DATA"
if [ -d /srv/web/data ] && [ -n "$(find /srv/web/data -mindepth 1 -print -quit 2>/dev/null)" ]; then
  if [ -n "$(find "$DATA" -mindepth 1 -print -quit 2>/dev/null)" ]; then
    echo 'Both /srv/web/data and /website/data contain files; refusing an ambiguous automatic migration.' >&2
    exit 1
  fi
  rsync -a /srv/web/data/ "$DATA/"
  rm -rf /srv/web/data
fi
if [ -s "$STATE/current-slot" ] && [ ! -f "$STATE/state-initialized" ]; then touch "$STATE/state-initialized"; fi
rmdir /srv/web 2>/dev/null || true

cd "$REPO"
git checkout -B main >/dev/null 2>&1 || true
if [ ! -f package.json ]; then
cat >package.json <<'PACKAGE'
{
  "name": "web-server",
  "private": true,
  "scripts": { "start": "node server.js" }
}
PACKAGE
fi
if [ ! -f server.js ]; then
cat >server.js <<'SERVER'
const http = require('node:http');
const port = Number(process.env.PORT || 3001);
http.createServer((req, res) => {
  res.writeHead(200, { 'content-type': 'text/plain; charset=utf-8' });
  res.end('ready\n');
}).listen(port, '127.0.0.1');
SERVER
fi

touch .gitignore
grep -qxF 'node_modules/' .gitignore || echo 'node_modules/' >> .gitignore
grep -qxF '.env' .gitignore || echo '.env' >> .gitignore
grep -qxF '*.log' .gitignore || echo '*.log' >> .gitignore
grep -qxF 'data/' .gitignore || echo 'data/' >> .gitignore
# Ignore applies only to untracked files. If an older site accidentally tracked
# data/, remove it from the index while leaving the runtime files in place.
git rm -r --cached -f --ignore-unmatch data >/dev/null 2>&1 || true

cat >.hermes.md <<'HERMES'
` + hermesContext + `HERMES
SOUL=/root/.hermes/SOUL.md
SKILL=/root/.hermes/skills/farzher-web-development/SKILL.md
if [ -f "$SOUL" ]; then sed -i 's#/srv/web/repo#/website#g; s#/srv/web/data#/website/data#g' "$SOUL"; fi
if [ -f "$SKILL" ]; then sed -i 's#/srv/web/repo#/website#g; s#/srv/web/data#/website/data#g' "$SKILL"; fi
if [ -f "$SKILL" ] && ! grep -q '^## Persistent application files$' "$SKILL"; then
cat >>"$SKILL" <<'SKILLDATA'

## Persistent application files
- The complete website working tree is /website.
- /website/data is the dedicated runtime-data directory and is deliberately ignored by Git as data/.
- Node receives DATA_DIR=/website/data. Use DATA_DIR for uploads, images, avatars, attachments, generated media, exports, and other durable files.
- Use PostgreSQL for structured/queryable application state. Do not put large file blobs in PostgreSQL solely for persistence unless the user explicitly asks.
- Never git add -f data, remove the data/ ignore rule, put runtime uploads in tracked source directories, or run git clean -fdx against /website.
- Do not create symlinks in DATA_DIR; the backup system rejects them.
- /usr/local/bin/backup-web snapshots PostgreSQL and data/ together; /usr/local/bin/restore-web restores both.
- If the user asks for an upload page or similar feature, file bytes go under DATA_DIR and metadata can go in PostgreSQL.
SKILLDATA
fi

cat >ops/deploy.sh <<'DEPLOY'
` + buildDeployScript() + `DEPLOY
cat >ops/backup.sh <<'BACKUP'
` + buildBackupScript() + `BACKUP
cat >ops/restore.sh <<'RESTORE'
` + buildRestoreScript() + `RESTORE
chmod +x ops/deploy.sh ops/backup.sh ops/restore.sh
ln -sf "$REPO/ops/deploy.sh" /usr/local/bin/deploy-web
ln -sf "$REPO/ops/backup.sh" /usr/local/bin/backup-web
ln -sf "$REPO/ops/restore.sh" /usr/local/bin/restore-web

cat >/etc/systemd/system/web-backup.service <<'SERVICE'
[Unit]
Description=Web PostgreSQL and persistent-file backup to GitHub
After=postgresql.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/backup-web
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
SERVICE
cat >/etc/systemd/system/web-backup.timer <<'TIMER'
[Unit]
Description=Daily web state backup

[Timer]
OnCalendar=*-*-* 03:15:00
Persistent=true
RandomizedDelaySec=15m

[Install]
WantedBy=timers.target
TIMER
systemctl daemon-reload
systemctl disable --now web-backup.timer >/dev/null 2>&1 || true

cat >/etc/nginx/sites-available/web <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name ` + serverName + `;
    location / {
        proxy_pass http://web_backend;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
NGINX
ln -sf /etc/nginx/sites-available/web /etc/nginx/sites-enabled/web
rm -f /etc/nginx/sites-enabled/default

git config user.name Hermes
git config user.email ` + shellQuote(adminEmail) + `
git add -A
if ! git diff --cached --quiet; then
  git commit -m 'Configure web server' >/dev/null
  git push -u origin HEAD:main
fi
hermes config set terminal.cwd /website >/dev/null

# A brand-new disk has no state marker/current slot. Restore remote state before
# the first deployment and before creating any new backup. Existing servers are
# adopted through the migrated current-slot marker instead of being overwritten.
if [ ! -f "$STATE/state-initialized" ]; then
  if [ -s "$STATE/current-slot" ]; then
    touch "$STATE/state-initialized"
  else
    /usr/local/bin/restore-web latest --no-restart
    touch "$STATE/state-initialized"
  fi
fi
/usr/local/bin/deploy-web
/usr/local/bin/backup-web
systemctl enable --now web-backup.timer >/dev/null
`
}
