package main

func buildRestoreScript() string {
	return `#!/bin/bash
set -Eeuo pipefail
umask 077
APP=/website/app
DATA=/website/data
STATE=/var/lib/website
REMOTE="$(git -C "$APP" remote get-url origin)"
TARGET="${1:-latest}"
NO_RESTART=0
if [ "${2:-}" = '--no-restart' ]; then NO_RESTART=1; fi
exec 9>/run/lock/web-state.lock
flock -n 9 || { echo 'Another deploy, backup, or restore is already running.' >&2; exit 1; }
install -d -m 0755 "$STATE"
if [ "$NO_RESTART" -eq 1 ] && systemctl is-active --quiet web; then
  echo '--no-restart is only valid while the web service is stopped.' >&2
  exit 1
fi
if ! git ls-remote --exit-code "$REMOTE" refs/heads/backup >/dev/null 2>&1; then
  echo 'No backup branch exists yet; starting with fresh server state.'
  exit 0
fi
TMP="$(mktemp -d /var/tmp/web-restore.XXXXXX)"
DATA_NEW="$STATE/.data-restore.$$"
DATA_OLD="$STATE/.data-before-restore.$$"
trap 'rm -rf "$TMP" "$DATA_NEW"; sudo -u postgres dropdb --if-exists web_restore >/dev/null 2>&1 || true' EXIT

git clone -q --depth 1 --filter=blob:none --no-checkout --single-branch --branch backup "$REMOTE" "$TMP/repo"
cd "$TMP/repo"
if [ "$TARGET" = latest ]; then
  DAILY_TREE="$(git ls-tree HEAD daily | awk '$2 == "tree" { print $3; exit }')"
  [ -n "$DAILY_TREE" ] || { echo 'Backup branch has no daily snapshots.' >&2; exit 1; }
  NAME="$(git ls-tree --name-only "$DAILY_TREE" | sort -r | head -1)"
  [ -n "$NAME" ] || { echo 'Backup branch has no daily snapshots.' >&2; exit 1; }
  TARGET="daily/$NAME"
fi
case "$TARGET" in
  daily/*|weekly/*|monthly/*|yearly/*) ;;
  *) echo 'Use latest or daily/DATE, weekly/WEEK, monthly/MONTH, yearly/YEAR.' >&2; exit 1 ;;
esac
TYPE="$(git cat-file -t "HEAD:$TARGET" 2>/dev/null || true)"
[ "$TYPE" = tree ] || { echo "Snapshot not found: $TARGET" >&2; exit 1; }

DB_PREFIX="$TARGET/database"
FILES_PREFIX="$TARGET/files"
HERMES_PREFIX="$TARGET/hermes"
[ "$(git cat-file -t "HEAD:$DB_PREFIX" 2>/dev/null || true)" = tree ] || { echo 'Snapshot is missing database state.' >&2; exit 1; }
[ "$(git cat-file -t "HEAD:$FILES_PREFIX" 2>/dev/null || true)" = tree ] || { echo 'Snapshot is missing persistent-file state.' >&2; exit 1; }
DB_TREE="$(git rev-parse "HEAD:$DB_PREFIX")"
DB_SINGLE=0
if git ls-tree --name-only "$DB_TREE" | grep -Fxq database.dump; then DB_SINGLE=1; fi
mapfile -t DB_PARTS < <(git ls-tree --name-only "$DB_TREE" | grep '^database\.dump\.part-' | sort)
if [ "$DB_SINGLE" -ne 1 ] && [ "${#DB_PARTS[@]}" -eq 0 ]; then
  echo 'Selected snapshot has no PostgreSQL dump.' >&2
  exit 1
fi
stream_database() {
  if [ "$DB_SINGLE" -eq 1 ]; then
    git cat-file blob "HEAD:$DB_PREFIX/database.dump"
  else
    local PART
    for PART in "${DB_PARTS[@]}"; do git cat-file blob "HEAD:$DB_PREFIX/$PART"; done
  fi
}
stream_database | pg_restore -l >/dev/null

sudo -u postgres dropdb --if-exists web_restore
sudo -u postgres createdb -O root web_restore
stream_database | sudo -u postgres pg_restore --exit-on-error -d web_restore

install -d -m 0750 "$DATA_NEW"
FILES_TREE="$(git rev-parse "HEAD:$FILES_PREFIX")"
while IFS= read -r -d '' ENTRY; do
  META="${ENTRY%%$'\t'*}"
  REL="${ENTRY#*$'\t'}"
  case "$REL" in .web-backup-large/*) continue;; esac
  case "$REL" in ''|/*|..|../*|*/..|*/../*) echo "Unsafe backup path: $REL" >&2; exit 1;; esac
  MODE="${META%% *}"
  REST="${META#* }"; TYPE="${REST%% *}"; SHA="${REST##* }"
  [ "$TYPE" = blob ] || continue
  DEST="$DATA_NEW/$REL"
  mkdir -p "$(dirname "$DEST")"
  git cat-file blob "$SHA" >"$DEST"
  if [ "$MODE" = 100755 ]; then chmod 755 "$DEST"; else chmod 644 "$DEST"; fi
done < <(git ls-tree -r -z "$FILES_TREE")

MANIFEST_PATH="$FILES_PREFIX/.web-backup-large/manifest.tsv"
if git cat-file -e "HEAD:$MANIFEST_PATH" 2>/dev/null; then
  MANIFEST="$TMP/large-files.tsv"
  git cat-file blob "HEAD:$MANIFEST_PATH" >"$MANIFEST"
  while IFS=$'\t' read -r PATH_B64 ID PERM; do
    [ -n "$PATH_B64" ] || continue
    REL="$(printf '%s' "$PATH_B64" | base64 -d)"
    case "$REL" in ''|/*|..|../*|*/..|*/../*) echo "Unsafe backup path: $REL" >&2; exit 1;; esac
    PART_TREE="$(git rev-parse "HEAD:$FILES_PREFIX/.web-backup-large/$ID")"
    DEST="$DATA_NEW/$REL"
    mkdir -p "$(dirname "$DEST")"
    : >"$DEST"
    while read -r MODE TYPE SHA PART; do
      [ "$TYPE" = blob ] || continue
      git cat-file blob "$SHA" >>"$DEST"
    done < <(git ls-tree "$PART_TREE" | sort -k4)
    chmod "$PERM" "$DEST"
  done <"$MANIFEST"
fi

HERMES_PRESENT=0
HERMES_NEW="$TMP/hermes"
if [ "$(git cat-file -t "HEAD:$HERMES_PREFIX" 2>/dev/null || true)" = tree ]; then
  HERMES_PRESENT=1
  install -d -m 0700 "$HERMES_NEW"
  HERMES_TREE="$(git rev-parse "HEAD:$HERMES_PREFIX")"
  while IFS= read -r -d '' ENTRY; do
    META="${ENTRY%%$'\t'*}"
    REL="${ENTRY#*$'\t'}"
    case "$REL" in ''|/*|..|../*|*/..|*/../*) echo "Unsafe Hermes backup path: $REL" >&2; exit 1;; esac
    case "$REL" in SOUL.md|memories/MEMORY.md|memories/USER.md|project/.hermes.md|skills/*) ;;
      *) continue;;
    esac
    MODE="${META%% *}"
    REST="${META#* }"; TYPE="${REST%% *}"; SHA="${REST##* }"
    [ "$TYPE" = blob ] || continue
    DEST="$HERMES_NEW/$REL"
    mkdir -p "$(dirname "$DEST")"
    git cat-file blob "$SHA" >"$DEST"
    if [ "$MODE" = 100755 ]; then chmod 700 "$DEST"; else chmod 600 "$DEST"; fi
  done < <(git ls-tree -r -z "$HERMES_TREE")
fi

RESTART=0
if [ "$NO_RESTART" -eq 0 ] && [ -f "$STATE/initialized" ]; then RESTART=1; fi
rollback_database() {
  sudo -u postgres psql postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname IN ('web','web_before_restore') AND pid <> pg_backend_pid();" >/dev/null 2>&1 || true
  sudo -u postgres dropdb --if-exists web_failed >/dev/null 2>&1 || true
  if sudo -u postgres psql postgres -tAc "SELECT 1 FROM pg_database WHERE datname='web_before_restore'" | grep -q 1; then
    if sudo -u postgres psql postgres -tAc "SELECT 1 FROM pg_database WHERE datname='web'" | grep -q 1; then
      sudo -u postgres psql postgres -c 'ALTER DATABASE web RENAME TO web_failed;' >/dev/null 2>&1 || true
    fi
    sudo -u postgres psql postgres -c 'ALTER DATABASE web_before_restore RENAME TO web;' >/dev/null 2>&1 || true
    sudo -u postgres dropdb --if-exists web_failed >/dev/null 2>&1 || true
  fi
}
restart_web() {
  if [ "$RESTART" -eq 1 ]; then systemctl restart web >/dev/null 2>&1 || true; fi
}
rollback_state() {
  if [ "$RESTART" -eq 1 ]; then systemctl stop web >/dev/null 2>&1 || true; fi
  rollback_database
  rm -rf "$DATA"
  if [ -d "$DATA_OLD" ]; then mv "$DATA_OLD" "$DATA"; else install -d -m 0750 "$DATA"; fi
  restart_web
}
check_ready() {
  curl -fsS -o /dev/null --max-time 1 http://127.0.0.1:3000/healthz || return 1
  psql -d web -tAc 'SELECT 1' 2>/dev/null | grep -qx 1
}
restore_hermes() {
  [ "$HERMES_PRESENT" -eq 1 ] || return 0
  install -d -m 0700 /root/.hermes /root/.hermes/memories
  local NAME SRC TMPFILE
  for NAME in MEMORY.md USER.md; do
    SRC="$HERMES_NEW/memories/$NAME"
    if [ -f "$SRC" ]; then
      TMPFILE="/root/.hermes/memories/.$NAME.restore.$$"
      install -m 0600 "$SRC" "$TMPFILE"
      mv "$TMPFILE" "/root/.hermes/memories/$NAME"
    else
      rm -f "/root/.hermes/memories/$NAME"
    fi
  done
  if [ -f "$HERMES_NEW/SOUL.md" ]; then
    TMPFILE="/root/.hermes/.SOUL.md.restore.$$"
    install -m 0600 "$HERMES_NEW/SOUL.md" "$TMPFILE"
    mv "$TMPFILE" /root/.hermes/SOUL.md
  fi
  local SKILLS_NEW="/root/.hermes/.skills-restore.$$" SKILLS_OLD="/root/.hermes/.skills-before-restore.$$"
  rm -rf "$SKILLS_NEW" "$SKILLS_OLD"
  install -d -m 0700 "$SKILLS_NEW"
  if [ -d "$HERMES_NEW/skills" ]; then cp -a "$HERMES_NEW/skills/." "$SKILLS_NEW/"; fi
  if [ -d /root/.hermes/skills ]; then mv /root/.hermes/skills "$SKILLS_OLD"; fi
  mv "$SKILLS_NEW" /root/.hermes/skills
  rm -rf "$SKILLS_OLD"
  if [ -f "$HERMES_NEW/project/.hermes.md" ] && ! cmp -s "$HERMES_NEW/project/.hermes.md" "$APP/.hermes.md"; then
    echo 'Backup contains a different .hermes.md; current managed project rules were kept.'
  fi
}

if [ "$RESTART" -eq 1 ]; then systemctl stop web; fi
sudo -u postgres psql postgres -v ON_ERROR_STOP=1 -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname IN ('web','web_restore','web_before_restore') AND pid <> pg_backend_pid();" >/dev/null
sudo -u postgres dropdb --if-exists web_before_restore
sudo -u postgres psql postgres -v ON_ERROR_STOP=1 -c 'ALTER DATABASE web RENAME TO web_before_restore;'
if ! sudo -u postgres psql postgres -v ON_ERROR_STOP=1 -c 'ALTER DATABASE web_restore RENAME TO web;'; then
  rollback_database
  restart_web
  echo 'Restore rolled back because the restored database could not be activated.' >&2
  exit 1
fi
rm -rf "$DATA_OLD"
if [ -d "$DATA" ] && ! mv "$DATA" "$DATA_OLD"; then
  rollback_database
  restart_web
  echo 'Restore rolled back because the current data directory could not be moved.' >&2
  exit 1
fi
if ! mv "$DATA_NEW" "$DATA"; then
  rollback_database
  if [ -d "$DATA_OLD" ]; then mv "$DATA_OLD" "$DATA"; else install -d -m 0750 "$DATA"; fi
  restart_web
  echo 'Restore rolled back because the restored persistent files could not be activated.' >&2
  exit 1
fi

if [ "$RESTART" -eq 1 ]; then
  if ! systemctl restart web; then rollback_state; echo 'Restore rolled back because the web service would not restart.' >&2; exit 1; fi
  READY=0
  for _ in $(seq 1 30); do
    if check_ready; then READY=1; break; fi
    sleep 0.1
  done
  if [ "$READY" != 1 ]; then
    /usr/local/bin/server-status --logs >&2 || true
    rollback_state
    echo 'Restore rolled back because the restored site did not become ready.' >&2
    exit 1
  fi
fi
sudo -u postgres dropdb --if-exists web_before_restore >/dev/null
rm -rf "$DATA_OLD"
restore_hermes
printf 'Restored %s (PostgreSQL + /website/data%s).\n' "$TARGET" "$([ "$HERMES_PRESENT" -eq 1 ] && printf ' + Hermes knowledge')"
`
}
