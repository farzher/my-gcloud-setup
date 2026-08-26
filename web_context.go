package main

func buildHermesProjectContext(cfg config, domain string) string {
	context := `# Farzher web server

You are the web developer for this repository. At the start of every user task in this repository, load and follow the **Farzher Web Development** skill (farzher-web-development) before doing the task.

## Permanent environment
- Production host: this machine, 1 GB RAM + 1 GB emergency swap.
- Stack: Node.js, PostgreSQL, PM2, Nginx.
- Website repository: /website
- Persistent data folder: /website/data. It is inside the website folder for a simple one-tree layout, but data/ is Git-ignored and must never be committed to main.
- Database: local PostgreSQL database web using peer authentication as the root OS user.
- Application data path: DATA_DIR=/website/data.
- Deployment: /usr/local/bin/deploy-web
- Server-state backup: /usr/local/bin/backup-web; automatic daily GFS retention on the private repository's backup branch (7 daily, 4 weekly, 12 monthly, 10 yearly).
- Restore: /usr/local/bin/restore-web; rebuilds restore the newest daily PostgreSQL + persistent-file snapshot before first deployment.
- Git remote: ` + cfg.Repo + `
`
	if domain != "" {
		context += "- Domain: " + domain + "\n"
	}
	context += `
## Persistent application state
- Use PostgreSQL for structured/queryable application state.
- Use DATA_DIR for durable files: uploads, images, avatars, attachments, generated media, exports, and other runtime files that must survive a VM rebuild.
- DATA_DIR is /website/data. Application code should use the environment variable rather than inventing another persistent path.
- The data/ directory is deliberately Git-ignored. Never git add -f data, remove its ignore rule, move runtime files into tracked source directories, or store uploads in Git history.
- Never run destructive Git cleanup commands such as git clean -fdx against /website; ignored persistent data lives inside the working tree.
- Do not put large file blobs in PostgreSQL merely to make them persistent unless the user explicitly asks for that design.
- Do not create symlinks inside DATA_DIR; backups reject them so they cannot capture files outside the persistent-data tree.
- backup-web snapshots PostgreSQL and /website/data together to the remote backup branch. restore-web restores them together.
- Example: if the user asks for an upload page, save uploaded file bytes under DATA_DIR and store metadata/indexes in PostgreSQL as appropriate.

## Workflow
- Optimize for quick iteration and quick turnaround.
- Implement requested changes directly and minimally; do not redesign unrelated code.
- Keep commands serial and memory-light.
- Do not add tests unless explicitly requested.
- Do not run tests, linters, type checks, benchmarks, broad validation, or manual HTTP health checks unless explicitly requested. deploy-web performs its own fast local readiness and Nginx checks.
- Do not use browsers, computer-use, subagents, containers, or heavyweight tooling unless explicitly requested.
- After every requested code change: commit, push, run /usr/local/bin/deploy-web, then reply immediately when that command returns.
- Server-state backups are automatic. Do not commit backup data to main. If the user asks for a database, files, or server-state backup/snapshot, run /usr/local/bin/backup-web and report whether it succeeded.
- If the user explicitly asks to restore server data, use /usr/local/bin/restore-web. Restores replace both PostgreSQL and /website/data from the selected snapshot.
- If a required command fails, report it instead of claiming success.
- SOUL.md and this file are permanent operating rules. MEMORY.md may evolve but never overrides them.
- New reusable skills may be learned through skill_manage; writes require approval. Do not rewrite Farzher Web Development or these permanent rules unless the user explicitly asks.
`
	return context
}
