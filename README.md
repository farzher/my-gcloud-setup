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
- Node.js, PostgreSQL, systemd, Nginx
- Certbot / Let's Encrypt when a domain is configured
- lean Hermes Agent using ChatGPT/Codex
- one private GitHub repository with a write-enabled VM deploy key

## Website layout

Everything application-specific lives under one top-level folder, with a hard boundary between Git and runtime data:

```text
/website/
├── app/             # Git repository
│   ├── .git/
│   ├── .hermes.md
│   ├── ops/
│   │   ├── ship.sh
│   │   ├── deploy.sh
│   │   ├── backup.sh
│   │   └── restore.sh
│   └── ...source files
└── data/            # persistent runtime files, never Git
```

Durable state is split cleanly:

- source and ops -> `/website/app` -> private GitHub `main`
- structured data -> PostgreSQL database `web`
- durable files -> `/website/data`
- PostgreSQL + durable-file snapshots -> private GitHub `backup`

Node receives:

```text
DATABASE_URL=postgresql:///web?host=/var/run/postgresql
DATA_DIR=/website/data
```

## Runtime and deployment

Nginx proxies directly to one systemd-managed Node process on `127.0.0.1:3000`. systemd restarts the process if it crashes.

`/usr/local/bin/deploy-web` installs production dependencies only when package files changed, restarts `web.service`, and waits for a successful local HTTP response before returning.

`/usr/local/bin/ship-web "message"` is the normal development finish command: it stages all changes, commits, pushes, then runs `deploy-web`. Any failure stops the chain.

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

Restore builds replacement state first, swaps it in only after validation, and rolls back if the restored site does not return successfully.

Before a rebuild deletes the VM disk, the app creates a fresh remote server-state backup. A new VM then reinstalls the system, clones `main` to `/website/app`, restores the newest snapshot to PostgreSQL + `/website/data`, deploys, and re-enables automatic backups.

Filesystem layouts are not migrated in place. Rebuild is the clean cutover path when the managed layout changes.

## Hermes

All custom Hermes setup is kept in `hermes.go`: model/settings, the short global `SOUL.md`, and the generated `/website/app/.hermes.md` project rules.

There is no always-loaded custom web-development skill. Project rules stay short and tell Hermes to finish code changes with one `ship-web` command. Hermes can still learn/use other skills when they are actually relevant.
