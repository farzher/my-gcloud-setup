package main

func buildWebProvisionScript(cfg config, serverName, hermesContext string) string {
	return `set -Eeuo pipefail
REPO=/website
DATA="$REPO/data"
STATE=/var/lib/website
install -d -m 0755 "$STATE"
mkdir -p "$REPO/ops"
install -d -m 0750 "$DATA"

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

cat >.hermes.md <<'HERMES'
` + hermesContext + `HERMES
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

# A fresh VM restores remote state before the first deployment or backup.
# The marker prevents repeated setup on the same VM from restoring over live state.
if [ ! -f "$STATE/initialized" ]; then
  /usr/local/bin/restore-web latest --no-restart
  touch "$STATE/initialized"
fi
/usr/local/bin/deploy-web
/usr/local/bin/backup-web
systemctl enable --now web-backup.timer >/dev/null
`
}
