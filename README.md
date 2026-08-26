# cloud

A small Windows TUI that turns a Google account into one managed Debian web server.

## Run

Double-click `run.bat` or run:

```bat
go run .
```

Go 1.25+ is required. `run.bat` pulls the latest source and installs GitHub CLI with winget when possible.

## First setup

1. Pick an already-authenticated Google account, or sign in with Browser / QR.
2. Enable/select billing.
3. Enter a domain such as `farzher.com`, or a plain server name if no domain is needed.
4. The Server screen provisions everything automatically.
5. Authorize ChatGPT when the Codex device QR/code appears.
6. Authorize GitHub CLI as `farzher` if needed.
7. If a domain was entered, point its **A record** at the static IP shown by the DNS step and press `r` to continue.
8. HTTPS is then issued automatically with Certbot/Let's Encrypt.

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

The app creates a private repository under `farzher`, generates an Ed25519 key on the VM, and registers it as a write-enabled GitHub deploy key. The website lives at `/srv/web/repo`.

## Hermes

Hermes is intentionally lean for the 1 GB VM:

- `~/.hermes/SOUL.md` defines the permanent identity: Farzher's fast, low-memory web developer.
- `/srv/web/repo/.hermes.md` contains permanent project/stack/deployment rules and instructs Hermes to load **Farzher Web Development** for every task in the repo.
- `~/.hermes/skills/farzher-web-development/SKILL.md` contains the quick-iteration procedure.
- `MEMORY.md` remains enabled for facts Hermes learns over time; permanent rules do not depend on mutable memory.
- Hermes may learn additional skills, but `skills.write_approval=true` stages skill changes for approval rather than silently applying them.

Normal development flow:

```text
edit -> commit -> push -> /usr/local/bin/deploy-web -> reply
```

No tests, linting, type checking, broad validation, or HTTP health checks are run unless explicitly requested.

## Deployment

`/usr/local/bin/deploy-web` uses one checkout and two PM2 slots:

- blue: port 3001
- green: port 3002

A deployment starts the inactive slot from the same `/srv/web/repo` checkout, switches the tiny Nginx upstream file, reloads Nginx, then removes the old process. Source code is not duplicated into blue/green directories.

PostgreSQL database `web` uses local peer authentication for the root-run app. Node is capped to a small heap and PM2 restarts an app that exceeds its memory limit.

## Database backups

`/usr/local/bin/backup-web` creates a consistent compressed PostgreSQL snapshot with `pg_dump -Fc`, plus PostgreSQL globals. A systemd timer runs it daily around 03:15 and pushes snapshots to the private repo's `backup` branch.

Retention is grandfather-father-son style:

- 7 daily
- 4 weekly
- 12 monthly
- 10 yearly

The same snapshot is reused for daily/weekly/monthly/yearly retention, so Git deduplicates identical blobs. The `backup` branch is force-rewritten as a single root commit on each run, which keeps the retained snapshots without accumulating dump history. Database dumps never go into `main`.

Run `/usr/local/bin/backup-web` manually for an immediate snapshot. Hermes is explicitly instructed to run this command whenever you ask it to make a database backup or snapshot.

If a compressed database dump grows beyond GitHub's normal per-file limit, the backup script automatically splits it into ~90 MiB `database.dump.part-*` files. Restore by concatenating the parts into `pg_restore`; backups do not stop merely because the database exceeds 100 MB.

## Rebuilds

Rebuild deletes only the VM and keeps the static IP, project, and private GitHub backup. The normal idempotent provisioning flow recreates the machine and clones the repo back onto it.
