# cloud

A small Windows TUI that turns a Google account into one reproducible Debian web server.

## Run

Double-click `run.bat` or run:

```bat
go run .
```

Go 1.25+ is required. `run.bat` pulls the latest source and installs GitHub CLI with winget when possible. If Git cannot fast-forward, the launcher stops instead of running stale source.

## First setup

1. Pick an already-authenticated Google account, or sign in with Browser / QR.
2. Enable/select billing.
3. Enter a domain such as `farzher.com`, or a plain server name if no domain is needed.
4. The Server screen provisions everything automatically.
5. Authorize ChatGPT when the Codex device QR/code appears.
6. Authorize GitHub CLI as `farzher` if needed.
7. If a domain was entered, point its **A record** at the static IP shown by the DNS step and press `r` to continue.
8. HTTPS is issued automatically with Certbot/Let's Encrypt.

The TUI warns before creating anything if it can see unrelated Compute Engine VMs.

## Result

The managed Google Cloud project contains:

- Debian 13 `e2-micro`
- 30 GB `pd-standard`
- reserved Premium IPv4
- TCP 22 / 80 / 443
- 1 GB emergency swap
- Node.js, PostgreSQL, PM2, Nginx
- Certbot + automatic Let's Encrypt renewal when a domain is configured
- Hermes Agent installed without browser/computer-use components or bundled skills
- ChatGPT/Codex OAuth using `gpt-5.6-sol` with medium reasoning
- `stephenkamenar@gmail.com` as Project Owner

The app creates one private repository under `farzher`, generates an Ed25519 key on the VM, and registers it as a write-enabled GitHub deploy key.

## One website tree

The entire website working tree is simply:

```text
/website/
├── .git/
├── .gitignore
├── .hermes.md
├── data/          # persistent runtime files; Git-ignored
├── ops/
│   ├── deploy.sh
│   ├── backup.sh
│   └── restore.sh
└── ...source files
```

`/website` is the private Git repository. `/website/data` deliberately lives inside that tree for a simple layout, but `.gitignore` contains `data/`, so uploads and generated runtime files never enter `main` or its Git history.

Node receives `DATA_DIR=/website/data`. Durable state therefore has three clear forms:

- tracked source/ops code -> private GitHub `main`
- structured application data -> PostgreSQL database `web`
- durable files -> `/website/data` -> private GitHub `backup` snapshots

Tiny operational markers such as the current blue/green slot live outside the site at `/var/lib/website`; they are disposable and recreated by provisioning.

## Hermes

Hermes is intentionally lean for the 1 GB VM:

- `~/.hermes/SOUL.md` defines the permanent identity: Farzher's fast, low-memory web developer.
- `/website/.hermes.md` contains permanent project/stack/deployment/state rules and instructs Hermes to load **Farzher Web Development** for every task in the repo.
- `~/.hermes/skills/farzher-web-development/SKILL.md` contains the quick-iteration procedure and persistent-file rules.
- `MEMORY.md` remains enabled for facts Hermes learns over time; permanent rules do not depend on mutable memory.
- Hermes may learn additional skills, but `skills.write_approval=true` stages skill changes for approval rather than silently applying them.

Hermes explicitly knows that an upload page, attachment system, generated-image feature, export feature, etc. stores file bytes under `DATA_DIR` and structured metadata in PostgreSQL when useful. It is also told never to force-add `data/` to Git or run `git clean -fdx` against `/website`.

Normal development flow:

```text
edit -> commit -> push -> /usr/local/bin/deploy-web -> reply
```

The agent does not perform manual post-deploy verification. `deploy-web` itself performs fast local readiness and Nginx checks before switching traffic.

## Deployment

`/usr/local/bin/deploy-web` is a symlink to `/website/ops/deploy.sh`. It uses one checkout and two PM2 slots:

- blue: port 3001
- green: port 3002

A deployment starts the inactive slot from `/website` and waits briefly for it to answer a local HTTP request. If it does not become ready, the failed slot is removed and the live Nginx upstream is never changed.

If the process is ready, the script writes the new upstream, validates Nginx, reloads it, then removes the old PM2 slot. Any Nginx validation/reload failure restores the previous upstream and leaves the old slot live. `flock` prevents simultaneous deployments.

Node runs with `DATABASE_URL=postgresql:///web?host=/var/run/postgresql` and `DATA_DIR=/website/data`.

## Server-state backups

`/usr/local/bin/backup-web` is a symlink to `/website/ops/backup.sh`. It snapshots both:

- PostgreSQL (`pg_dump -Fc` plus PostgreSQL globals)
- `/website/data`

A systemd timer runs daily around 03:15 and pushes snapshots to the private repository's `backup` branch. `/usr/local/bin/backup-web` triggers an immediate manual snapshot, and Hermes runs it when asked for a database/files/server-state backup.

Retention:

- 7 daily
- 4 weekly
- 12 monthly
- 10 yearly

Persistent files are stored as separate Git blobs rather than one giant tarball, so unchanged files deduplicate naturally across snapshots. Individual database chunks or persistent files larger than about 90 MiB are split into GitHub-safe parts.

Retained snapshot blobs stay on GitHub. The VM fetches only backup commit/tree metadata when creating a new snapshot, and temporary new-snapshot objects are removed after the push. The `backup` branch is force-rewritten as a single root commit each run so expired backup history does not accumulate.

`backup-web` and `restore-web` share a lock so a manual restore cannot race the scheduled backup.

## Restore and rebuild

`/usr/local/bin/restore-web` is a symlink to `/website/ops/restore.sh`. It restores PostgreSQL and `/website/data` together. It supports `latest` (newest daily snapshot) or an explicit retained path such as `daily/2026-08-26` or `monthly/2026-08`.

The restore streams selected Git blobs directly into a temporary database and replacement data directory instead of checking out another complete copy of the backup. Only after everything validates does it swap the state. On a live server, the current PM2 slot is restarted and checked; if it does not come back, the database/files swap is rolled back.

A VM rebuild is automatic:

```text
new disk
-> install system/Hermes
-> clone main to /website
-> install site ops scripts
-> restore newest remote PostgreSQL + /website/data snapshot
-> deploy
-> create a fresh backup
-> enable the daily timer
```

