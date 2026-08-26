package main

func buildWebSetupScript(cfg config) string {
	domain := cfg.domainFor(cfg.Account)
	serverName := "_"
	if domain != "" {
		serverName = domain
	}
	hermesContext := `# Farzher web server

You are the web developer for this repository. At the start of every user task in this repository, load and follow the **Farzher Web Development** skill (` + "`farzher-web-development`" + `) before doing the task.

## Permanent environment
- Production host: this machine, 1 GB RAM + 1 GB emergency swap.
- Stack: Node.js, PostgreSQL, PM2, Nginx.
- Repository: /srv/web/repo
- Database: local PostgreSQL database ` + "`web`" + ` using peer authentication as the root OS user.
- Deployment: /usr/local/bin/deploy-web
- Database backup: /usr/local/bin/backup-web; automatic daily GFS retention on GitHub branch backup (7 daily, 4 weekly, 12 monthly, 10 yearly).
- Git remote: ` + cfg.Repo + `
`
	if domain != "" {
		hermesContext += "- Domain: " + domain + "\n"
	}
	hermesContext += `
## Workflow
- Optimize for quick iteration and quick turnaround.
- Implement requested changes directly and minimally; do not redesign unrelated code.
- Keep commands serial and memory-light.
- Do not add tests unless explicitly requested.
- Do not run tests, linters, type checks, benchmarks, broad validation, or HTTP health checks unless explicitly requested.
- Do not use browsers, computer-use, subagents, containers, or heavyweight tooling unless explicitly requested.
- After every requested code change: commit, push, run /usr/local/bin/deploy-web, then reply immediately when that command returns.
- Database backups are automatic. Do not commit database dumps to main. If the user asks for a database backup or snapshot, run /usr/local/bin/backup-web and report whether it succeeded.
- If a required command fails, report it instead of claiming success.
- SOUL.md and this file are permanent operating rules. MEMORY.md may evolve but never overrides them.
- New reusable skills may be learned through skill_manage; writes require approval. Do not rewrite Farzher Web Development or these permanent rules unless the user explicitly asks.
`

	deploy := `#!/bin/bash
set -Eeuo pipefail
ROOT=/srv/web
REPO="$ROOT/repo"
cd "$REPO"
CURRENT="$(cat "$ROOT/current-slot" 2>/dev/null || true)"
if [ "$CURRENT" = blue ]; then NEXT=green; PORT=3002; else NEXT=blue; PORT=3001; fi
NAME="web-$NEXT"
export npm_config_audit=false npm_config_fund=false npm_config_jobs=1
export NODE_OPTIONS=--max-old-space-size=224
pkg_hash() { { cat package.json; [ ! -f package-lock.json ] || cat package-lock.json; } | sha256sum | awk '{print $1}'; }
HASH="$(pkg_hash)"
if [ ! -d node_modules ] || [ "$(cat "$ROOT/deps-hash" 2>/dev/null || true)" != "$HASH" ]; then
  npm install --omit=dev --no-audit --no-fund
  HASH="$(pkg_hash)"
  printf '%s\n' "$HASH" >"$ROOT/deps-hash"
fi
pm2 delete "$NAME" >/dev/null 2>&1 || true
PORT="$PORT" DATABASE_URL='postgresql:///web?host=/var/run/postgresql' NODE_OPTIONS=--max-old-space-size=224 \
  pm2 start npm --name "$NAME" --max-memory-restart 300M -- start >/dev/null
cat > /etc/nginx/conf.d/web-upstream.conf.tmp <<UPSTREAM
upstream web_backend {
    server 127.0.0.1:$PORT;
}
UPSTREAM
mv /etc/nginx/conf.d/web-upstream.conf.tmp /etc/nginx/conf.d/web-upstream.conf
systemctl reload nginx
if [ -n "$CURRENT" ] && [ "$CURRENT" != "$NEXT" ]; then pm2 delete "web-$CURRENT" >/dev/null 2>&1 || true; fi
printf '%s\n' "$NEXT" >"$ROOT/current-slot"
pm2 save --force >/dev/null
`

	backup := `#!/bin/bash
set -Eeuo pipefail
umask 077
ROOT=/srv/web
REPO="$ROOT/repo"
REMOTE="$(git -C "$REPO" remote get-url origin)"
TMP="$(mktemp -d /var/tmp/web-backup.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
GIT="$TMP/git"

mkdir -p "$GIT"
git -C "$GIT" init -q
git -C "$GIT" remote add origin "$REMOTE"
HAVE_BACKUP=0
if git -C "$GIT" ls-remote --exit-code origin refs/heads/backup >/dev/null 2>&1; then
  git -C "$GIT" -c protocol.version=2 fetch -q --depth=1 --filter=blob:none origin refs/heads/backup:refs/remotes/origin/backup
  HAVE_BACKUP=1
fi

# Stream the new dump straight into GitHub-safe chunks so the VM never holds
# both a large dump and a second split copy at the same time.
sudo -u postgres pg_dump -Fc web | split -b 94371840 -d -a 3 - "$TMP/database.dump.part-"
if ! compgen -G "$TMP/database.dump.part-*" >/dev/null; then
  echo 'pg_dump produced no backup data' >&2
  exit 1
fi
set -- "$TMP"/database.dump.part-*
if [ "$#" -eq 1 ]; then
  mv "$1" "$TMP/database.dump"
fi
sudo -u postgres pg_dumpall --globals-only | gzip -6 >"$TMP/globals.sql.gz"

cd "$GIT"
SNAPSHOT_ENTRIES="$TMP/snapshot.entries"
: >"$SNAPSHOT_ENTRIES"
if [ -f "$TMP/database.dump" ]; then
  SHA="$(git hash-object -w "$TMP/database.dump")"
  printf '100644 blob %s\tdatabase.dump\n' "$SHA" >>"$SNAPSHOT_ENTRIES"
else
  for FILE in "$TMP"/database.dump.part-*; do
    SHA="$(git hash-object -w "$FILE")"
    printf '100644 blob %s\t%s\n' "$SHA" "$(basename "$FILE")" >>"$SNAPSHOT_ENTRIES"
  done
fi
GLOBALS_SHA="$(git hash-object -w "$TMP/globals.sql.gz")"
printf '100644 blob %s\tglobals.sql.gz\n' "$GLOBALS_SHA" >>"$SNAPSHOT_ENTRIES"
SNAPSHOT_TREE="$(git mktree <"$SNAPSHOT_ENTRIES")"

category_tree() {
  local CATEGORY="$1" NAME="$2" KEEP="$3"
  local LIST="$TMP/$CATEGORY.list"
  : >"$LIST"
  if [ "$HAVE_BACKUP" -eq 1 ]; then
    local OLD_TREE
    OLD_TREE="$(git ls-tree refs/remotes/origin/backup "$CATEGORY" | awk '$2 == "tree" { print $3; exit }')"
    if [ -n "$OLD_TREE" ]; then
      while read -r MODE TYPE SHA ENTRY; do
        [ "$TYPE" = tree ] || continue
        [ "$ENTRY" = "$NAME" ] && continue
        printf '%s\t%s\n' "$ENTRY" "$SHA" >>"$LIST"
      done < <(git ls-tree "$OLD_TREE")
    fi
  fi
  printf '%s\t%s\n' "$NAME" "$SNAPSHOT_TREE" >>"$LIST"

  local SELECTED="$TMP/$CATEGORY.selected"
  sort -r "$LIST" | sed -n "1,${KEEP}p" >"$SELECTED"
  local ENTRIES="$TMP/$CATEGORY.entries"
  : >"$ENTRIES"
  sort "$SELECTED" | while IFS=$'\t' read -r ENTRY SHA; do
    [ -n "$ENTRY" ] || continue
    printf '040000 tree %s\t%s\n' "$SHA" "$ENTRY"
  done >"$ENTRIES"
  git mktree <"$ENTRIES"
}

DAY="$(date -u +%F)"
WEEK="$(date -u +%G-W%V)"
MONTH="$(date -u +%Y-%m)"
YEAR="$(date -u +%Y)"
DAILY_TREE="$(category_tree daily "$DAY" 7)"
WEEKLY_TREE="$(category_tree weekly "$WEEK" 4)"
MONTHLY_TREE="$(category_tree monthly "$MONTH" 12)"
YEARLY_TREE="$(category_tree yearly "$YEAR" 10)"

cat >"$TMP/README.md" <<'BACKUPREADME'
# Database backups

Automatic PostgreSQL snapshots for this server.

Retention:
- 7 daily
- 4 weekly
- 12 monthly
- 10 yearly

Each snapshot directory contains PostgreSQL globals plus a ` + "`pg_dump -Fc web`" + ` custom-format database dump. Dumps larger than about 90 MiB are automatically split into ` + "`database.dump.part-*`" + ` files so every GitHub blob stays below the normal per-file limit.

Restore an unsplit snapshot with:
` + "`pg_restore -d web database.dump`" + `

Restore a split snapshot with:
` + "`cat database.dump.part-* | pg_restore -d web`" + `

Restore globals separately with ` + "`gunzip -c globals.sql.gz | psql`" + ` if needed.

The VM stores only the new snapshot temporarily. Existing retained snapshot blobs stay on GitHub; each run fetches only the backup branch's Git tree metadata. The branch is rewritten as a single root commit every run so Git history does not accumulate old dumps.
BACKUPREADME

README_SHA="$(git hash-object -w "$TMP/README.md")"
ROOT_ENTRIES="$TMP/root.entries"
{
  printf '100644 blob %s\tREADME.md\n' "$README_SHA"
  printf '040000 tree %s\tdaily\n' "$DAILY_TREE"
  printf '040000 tree %s\tweekly\n' "$WEEKLY_TREE"
  printf '040000 tree %s\tmonthly\n' "$MONTHLY_TREE"
  printf '040000 tree %s\tyearly\n' "$YEARLY_TREE"
} >"$ROOT_ENTRIES"
TREE="$(git mktree <"$ROOT_ENTRIES")"
git config user.name 'Cloud Backup'
git config user.email 'backup@localhost'
COMMIT="$(printf 'Database backup %s\n' "$DAY" | git commit-tree "$TREE")"
git push -q --force origin "$COMMIT:refs/heads/backup"
`

	script := `set -Eeuo pipefail
REPO=/srv/web/repo
mkdir -p "$REPO/ops"
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
cat >.hermes.md <<'HERMES'
` + hermesContext + `HERMES
cat >ops/deploy.sh <<'DEPLOY'
` + deploy + `DEPLOY
chmod +x ops/deploy.sh
ln -sf "$REPO/ops/deploy.sh" /usr/local/bin/deploy-web
cat >/usr/local/bin/backup-web <<'BACKUP'
` + backup + `BACKUP
chmod +x /usr/local/bin/backup-web
cat >/etc/systemd/system/web-backup.service <<'SERVICE'
[Unit]
Description=Web PostgreSQL backup to GitHub
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
Description=Daily web PostgreSQL backup

[Timer]
OnCalendar=*-*-* 03:15:00
Persistent=true
RandomizedDelaySec=15m

[Install]
WantedBy=timers.target
TIMER
systemctl daemon-reload
systemctl enable --now web-backup.timer >/dev/null
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
hermes config set terminal.cwd /srv/web/repo >/dev/null
/usr/local/bin/deploy-web
/usr/local/bin/backup-web
`
	return script
}
