# cloud

A small Windows TUI that turns a Google account into one reproducible Debian web server.

## Run

Double-click `run.bat` or run:

```bat
go run .
```

Requires Go 1.25+. `run.bat` fast-forwards to the latest source and installs GitHub CLI with winget when possible. If Git cannot update cleanly, it stops instead of running stale code.

## Setup

1. Pick a Google account or sign in with Browser / QR.
2. Select billing.
3. Enter a domain such as `farzher.com`, or a plain server name.
4. Let the Server screen provision the machine.
5. Authorize ChatGPT/Codex and GitHub when prompted.
6. For a domain, point its A record at the shown static IP and retry.

The TUI warns before creating resources when it can see unrelated Compute Engine VMs.

## What it creates

- Debian 13 `e2-micro`
- 30 GB `pd-standard`
- reserved Premium IPv4
- TCP 22 / 80 / 443
- 1 GB emergency swap
- Node.js, PostgreSQL, PM2, Nginx
- Certbot / Let's Encrypt when a domain is configured
- lean Hermes Agent using ChatGPT/Codex
- one private GitHub repository with a write-enabled VM deploy key

## Website layout

```text
/website/
├── .git/
├── .gitignore
├── .hermes.md
├── data/          # persistent, Git-ignored
├── ops/
│   ├── deploy.sh
│   ├── backup.sh
│   └── restore.sh
└── ...source files
```

Durable state is split cleanly:

- source and ops -> private GitHub `main`
- structured data -> PostgreSQL database `web`
- durable files -> `/website/data`
- PostgreSQL + durable-file snapshots -> private GitHub `backup`

Node receives:

```text
DATABASE_URL=postgresql:///web?host=/var/run/postgresql
DATA_DIR=/website/data
```

## Deployment

`/usr/local/bin/deploy-web` uses blue/green PM2 slots on ports 3001 and 3002.

It starts the inactive slot, requires a successful local HTTP response, validates and reloads Nginx, then removes the old slot. Failed readiness or Nginx checks leave the previous slot live.

Deploy, backup, and restore share one lock so state-changing operations cannot overlap.

Dependencies use `npm ci` when `package-lock.json` exists. Without a lock file, deployment installs without generating one on the production checkout.

## Backups

`/usr/local/bin/backup-web` snapshots PostgreSQL and `/website/data` to the repository's `backup` branch.

Retention:

- 7 daily
- 4 weekly
- 12 monthly
- 10 yearly

A systemd timer runs daily around 03:15. Unchanged files deduplicate as Git blobs; individual database chunks or files above roughly 90 MiB are split into GitHub-safe parts.

## Restore and rebuild

`/usr/local/bin/restore-web` restores PostgreSQL and `/website/data` together from `latest` or an explicit retained snapshot such as `daily/2026-08-26`.

Restore builds replacement state first, swaps it in only after validation, and rolls back if the live site does not return successfully.

Before a rebuild deletes the VM disk, the app creates a fresh remote server-state backup. A new VM then reinstalls the system, clones `main`, restores the newest snapshot, deploys, and re-enables automatic backups.

## Hermes

All custom Hermes setup is kept in `hermes.go`: Hermes settings, `SOUL.md`, `/website/.hermes.md`, and the `farzher-web-development` skill. Edit that one file to change the managed Hermes behavior.

Hermes works from `/website` and is configured for the 1 GB VM. Persistent files belong under `DATA_DIR`; structured application state belongs in PostgreSQL.
