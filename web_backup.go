package main

func buildBackupScript() string {
	return `#!/bin/bash
set -Eeuo pipefail
umask 077
APP=/website/app
DATA=/website/data
REMOTE="$(git -C "$APP" remote get-url origin)"
exec 9>/run/lock/web-state.lock
flock -n 9 || { echo 'Another deploy, backup, or restore is already running.' >&2; exit 1; }
install -d -m 0750 "$DATA"
if find "$DATA" -type l -print -quit | grep -q .; then
  echo 'Persistent-data symlinks are not supported; remove symlinks from /website/data before backup.' >&2
  exit 1
fi
if [ -e "$DATA/.web-backup-large" ]; then
  echo '/website/data/.web-backup-large is reserved by the backup system.' >&2
  exit 1
fi
if [ -d /root/.hermes/skills ] && find /root/.hermes/skills -type l -print -quit | grep -q .; then
  echo 'Hermes skill symlinks are not supported by backup-web.' >&2
  exit 1
fi
TMP="$(mktemp -d /var/tmp/web-backup.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
GIT="$TMP/git"
mkdir -p "$GIT"
git -C "$GIT" init -q
git -C "$GIT" remote add origin "$REMOTE"
HAVE_BACKUP=0
if git -C "$GIT" ls-remote --exit-code origin refs/heads/backup >/dev/null 2>&1; then
  git -C "$GIT" -c protocol.version=2 fetch -q --depth=1 --filter=blob:none origin refs/heads/backup:refs/remotes/origin/backup
  git -C "$GIT" config remote.origin.promisor true
  git -C "$GIT" config remote.origin.partialclonefilter blob:none
  HAVE_BACKUP=1
fi

cd "$GIT"
CHUNK_BYTES=94371840
sudo -u postgres pg_dump -Fc web | split -b "$CHUNK_BYTES" -d -a 3 - "$TMP/database.dump.part-"
compgen -G "$TMP/database.dump.part-*" >/dev/null || { echo 'pg_dump produced no backup data' >&2; exit 1; }
set -- "$TMP"/database.dump.part-*
if [ "$#" -eq 1 ]; then mv "$1" "$TMP/database.dump"; fi

DB_ENTRIES="$TMP/database.entries"
: >"$DB_ENTRIES"
if [ -f "$TMP/database.dump" ]; then
  SHA="$(git hash-object -w "$TMP/database.dump")"
  printf '100644 blob %s\tdatabase.dump\n' "$SHA" >>"$DB_ENTRIES"
  rm -f "$TMP/database.dump"
else
  for FILE in "$TMP"/database.dump.part-*; do
    SHA="$(git hash-object -w "$FILE")"
    printf '100644 blob %s\t%s\n' "$SHA" "$(basename "$FILE")" >>"$DB_ENTRIES"
    rm -f "$FILE"
  done
fi
DATABASE_TREE="$(git mktree <"$DB_ENTRIES")"

# Persistent files are separate Git blobs, not a tarball, so unchanged files
# deduplicate naturally between retained snapshots. Files above 90 MiB are
# chunked under an internal tree and reconstructed by restore-web.
FILES_INDEX="$TMP/files.index"
GIT_INDEX_FILE="$FILES_INDEX" git read-tree --empty
LARGE_MANIFEST="$TMP/large-files.tsv"
: >"$LARGE_MANIFEST"
while IFS= read -r -d '' FILE; do
  REL="${FILE#"$DATA"/}"
  [ -n "$REL" ] || continue
  SIZE="$(stat -c %s "$FILE")"
  MODE=100644; PERM=644
  if [ -x "$FILE" ]; then MODE=100755; PERM=755; fi
  if [ "$SIZE" -le "$CHUNK_BYTES" ]; then
    SHA="$(git hash-object -w "$FILE")"
    GIT_INDEX_FILE="$FILES_INDEX" git update-index --add --cacheinfo "$MODE" "$SHA" "$REL"
  else
    ID="$(printf '%s' "$REL" | sha256sum | awk '{print $1}')"
    PARTDIR="$TMP/large/$ID"
    mkdir -p "$PARTDIR"
    split -b "$CHUNK_BYTES" -d -a 3 "$FILE" "$PARTDIR/part-"
    for PART in "$PARTDIR"/part-*; do
      SHA="$(git hash-object -w "$PART")"
      GIT_INDEX_FILE="$FILES_INDEX" git update-index --add --cacheinfo 100644 "$SHA" ".web-backup-large/$ID/$(basename "$PART")"
      rm -f "$PART"
    done
    printf '%s\t%s\t%s\n' "$(printf '%s' "$REL" | base64 -w0)" "$ID" "$PERM" >>"$LARGE_MANIFEST"
  fi
done < <(find "$DATA" -type f -print0)
if [ -s "$LARGE_MANIFEST" ]; then
  SHA="$(git hash-object -w "$LARGE_MANIFEST")"
  GIT_INDEX_FILE="$FILES_INDEX" git update-index --add --cacheinfo 100644 "$SHA" '.web-backup-large/manifest.tsv'
fi
FILES_TREE="$(GIT_INDEX_FILE="$FILES_INDEX" git write-tree)"

# Preserve curated Hermes knowledge, learned skills, SOUL.md, and the current
# project context without storing chat/session history, auth, config, or .env.
HERMES_INDEX="$TMP/hermes.index"
GIT_INDEX_FILE="$HERMES_INDEX" git read-tree --empty
add_hermes_file() {
  local SRC="$1" REL="$2" MODE=100644 SHA
  [ -f "$SRC" ] || return 0
  if [ -x "$SRC" ]; then MODE=100755; fi
  SHA="$(git hash-object -w "$SRC")"
  GIT_INDEX_FILE="$HERMES_INDEX" git update-index --add --cacheinfo "$MODE" "$SHA" "$REL"
}
add_hermes_file /root/.hermes/SOUL.md SOUL.md
add_hermes_file /root/.hermes/memories/MEMORY.md memories/MEMORY.md
add_hermes_file /root/.hermes/memories/USER.md memories/USER.md
add_hermes_file "$APP/AGENTS.md" project/AGENTS.md
if [ -d /root/.hermes/skills ]; then
  while IFS= read -r -d '' FILE; do
    add_hermes_file "$FILE" "skills/${FILE#/root/.hermes/skills/}"
  done < <(find /root/.hermes/skills -type f -print0)
fi
HERMES_TREE="$(GIT_INDEX_FILE="$HERMES_INDEX" git write-tree)"

SNAPSHOT_ENTRIES="$TMP/snapshot.entries"
{
  printf '040000 tree %s\tdatabase\n' "$DATABASE_TREE"
  printf '040000 tree %s\tfiles\n' "$FILES_TREE"
  printf '040000 tree %s\thermes\n' "$HERMES_TREE"
} >"$SNAPSHOT_ENTRIES"
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
# Server state backups

Automatic PostgreSQL + /website/data + curated Hermes snapshots.

Retention:
- 7 daily
- 4 weekly
- 12 monthly
- 10 yearly

Each snapshot contains database/, files/, and hermes/. PostgreSQL uses pg_dump -Fc; persistent files are individual Git blobs so unchanged files deduplicate between snapshots. Individual database chunks or persistent files larger than about 90 MiB are split into GitHub-safe parts and transparently reconstructed by restore-web.

Hermes snapshots include MEMORY.md, USER.md, learned skills, SOUL.md, and a recovery copy of /website/app/AGENTS.md. Chat/session history (state.db), credentials, config.yaml, and .env are intentionally excluded. restore-web restores memories, learned skills, and SOUL.md; the current managed AGENTS.md remains authoritative so an old backup cannot silently replace newer project rules.

The VM stores only the new snapshot temporarily. Existing retained blobs stay on GitHub; backup only fetches commit/tree metadata from the previous backup branch. The branch is rewritten as a single root commit every run so Git history does not accumulate expired snapshots.
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
COMMIT="$(printf 'Server backup %s\n' "$DAY" | git commit-tree "$TREE")"
git push -q --force origin "$COMMIT:refs/heads/backup"
printf 'Backed up PostgreSQL, %s, and Hermes knowledge.\n' "$DATA"
`
}
