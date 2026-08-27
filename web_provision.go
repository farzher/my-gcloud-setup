package main

func buildWebProvisionScript(cfg config, serverName, hermesContext string) string {
	return `set -Eeuo pipefail
APP=/website/app
DATA=/website/data
STATE=/var/lib/website
install -d -m 0755 /website "$STATE"
install -d -m 0750 "$DATA"
mkdir -p "$APP/ops"

cd "$APP"
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
const { execFile } = require('node:child_process');
const port = Number(process.env.PORT || 3000);
const databaseUrl = process.env.DATABASE_URL || 'postgresql:///web?host=/var/run/postgresql';

function databaseHealthy(callback) {
  execFile('/usr/bin/psql', [databaseUrl, '-tAc', 'SELECT 1'], { timeout: 1000 }, (error, stdout) => {
    callback(!error && stdout.trim() === '1');
  });
}

http.createServer((req, res) => {
  if (req.url === '/healthz') {
    databaseHealthy((databaseOK) => {
      res.writeHead(databaseOK ? 200 : 503, { 'content-type': 'application/json; charset=utf-8' });
      res.end(JSON.stringify({ web: 'ok', database: databaseOK ? 'ok' : 'error' }) + '\n');
    });
    return;
  }
  res.writeHead(200, { 'content-type': 'text/plain; charset=utf-8' });
  res.end('ready\n');
}).listen(port, '127.0.0.1');
SERVER
fi

touch .gitignore
sed -i '/^data\/$/d; /^\.env$/d' .gitignore
grep -qxF 'node_modules/' .gitignore || echo 'node_modules/' >> .gitignore
grep -qxF '*.log' .gitignore || echo '*.log' >> .gitignore

cat >AGENTS.md <<'AGENTS'
` + hermesContext + `AGENTS
cat >ops/deploy.sh <<'DEPLOY'
` + buildDeployScript() + `DEPLOY
cat >ops/ship.sh <<'SHIP'
` + buildShipScript() + `SHIP
cat >ops/status.sh <<'STATUS'
` + buildStatusScript() + `STATUS
cat >ops/backup.sh <<'BACKUP'
` + buildBackupScript() + `BACKUP
cat >ops/restore.sh <<'RESTORE'
` + buildRestoreScript() + `RESTORE
chmod +x ops/deploy.sh ops/ship.sh ops/status.sh ops/backup.sh ops/restore.sh
ln -sf "$APP/ops/deploy.sh" /usr/local/bin/deploy-web
ln -sf "$APP/ops/ship.sh" /usr/local/bin/ship-web
ln -sf "$APP/ops/status.sh" /usr/local/bin/server-status
ln -sf "$APP/ops/backup.sh" /usr/local/bin/backup-web
ln -sf "$APP/ops/restore.sh" /usr/local/bin/restore-web

cat >/etc/systemd/system/web.service <<'SERVICE'
[Unit]
Description=Web application
After=postgresql.service network.target

[Service]
WorkingDirectory=/website/app
Environment=PORT=3000
Environment=DATA_DIR=/website/data
Environment=DATABASE_URL=postgresql:///web?host=/var/run/postgresql
Environment=NODE_OPTIONS=--max-old-space-size=224
ExecStart=/usr/bin/npm start
Restart=on-failure
RestartSec=1
StandardOutput=journal
StandardError=journal
SyslogIdentifier=web

[Install]
WantedBy=multi-user.target
SERVICE

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
systemctl enable web.service >/dev/null
systemctl disable --now web-backup.timer >/dev/null 2>&1 || true

cat >/etc/nginx/sites-available/web <<'NGINX'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name ` + serverName + `;
    location / {
        proxy_pass http://127.0.0.1:3000;
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
nginx -t >/dev/null
systemctl reload nginx

git config user.name Hermes
git config user.email ` + shellQuote(adminEmail) + `
git add -A
if ! git diff --cached --quiet; then
  git commit -m 'Configure web server' >/dev/null
  git push -u origin HEAD:main
fi
hermes config set terminal.cwd "$APP" >/dev/null

# A fresh VM restores remote state before the first deployment or backup.
if [ ! -f "$STATE/initialized" ]; then
  /usr/local/bin/restore-web latest --no-restart
  touch "$STATE/initialized"
fi
/usr/local/bin/deploy-web
/usr/local/bin/backup-web
systemctl enable --now web-backup.timer >/dev/null
`
}
