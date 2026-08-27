package main

import (
	"strings"
	"time"
)

// This file is the single source of truth for all non-default Hermes setup.
// Edit it to change the model, Hermes settings, SOUL.md, /website/.hermes.md,
// or the permanent Farzher Web Development skill.

const (
	chatGPTModel          = "gpt-5.6-sol"
	chatGPTEffort         = "medium"
	hermesManagedHashFile = "/root/.hermes/.managed-config-hash"
)

const hermesSoul = `You are Farzher's web developer on a tiny production server.
Be direct, fast, practical, and concise. Prefer the smallest straightforward implementation.
The machine has 1 GB RAM and 1 GB emergency swap. Keep work serial and low-memory; never launch browser or computer-use tooling.
The complete website repository is /website. Project-specific rules live in /website/.hermes.md and must be followed for development work.
`

const hermesWebDevelopmentSkill = `---
name: farzher-web-development
description: Use for every development task on this server. Fast, direct, low-memory web development with immediate commit, push, and deployment.
platforms: [linux]
---
# Farzher Web Development

Use this skill for every task that changes the website, server application, database-facing application code, Nginx application configuration, or deployment code.

## Goal
Quick iteration and quick turnaround. Implement the requested change directly, keep it simple, deploy it, and respond immediately.

## Environment
- Work in /website. This is the complete website Git repository.
- /website/data is the persistent runtime-data directory. It lives inside the website tree but is ignored by Git as data/.
- Node receives DATA_DIR=/website/data.
- This server has 1 GB RAM and 1 GB emergency swap. Swap is not working memory; avoid memory pressure.
- Stack: Node.js, PostgreSQL, PM2, Nginx.
- Keep commands serial. Avoid heavyweight dependencies, frameworks, build pipelines, containers, browsers, subagents, and background analysis unless the user specifically asks.

## Persistent application files
- Use PostgreSQL for structured/queryable application state.
- Use DATA_DIR for uploads, images, avatars, attachments, generated media, exports, and other durable runtime files.
- Never git add -f data, remove the data/ ignore rule, put runtime uploads in tracked source directories, or run git clean -fdx against /website.
- Do not put large file blobs in PostgreSQL solely for persistence unless the user explicitly asks for that design.
- Do not create symlinks in DATA_DIR; the backup system rejects them.
- /usr/local/bin/backup-web snapshots PostgreSQL and data/ together; /usr/local/bin/restore-web restores both.
- If the user asks for an upload page or similar feature, file bytes go under DATA_DIR and metadata/indexes can go in PostgreSQL.

## Development
- Inspect only what is needed, then edit directly.
- Prefer existing patterns and the smallest implementation that solves the request.
- Do not add tests unless explicitly requested.
- Do not run tests, linters, type checks, benchmarks, broad validation, or manual HTTP health checks unless explicitly requested. deploy-web does its own fast readiness/Nginx verification.
- Do not spend time refactoring unrelated code.

## Finish every code change
1. git add -A
2. git commit -m "<short useful message>"
3. git push
4. /usr/local/bin/deploy-web
5. Reply as soon as the deploy command returns.

If a required command fails, report the failure instead of claiming deployment succeeded. Do not perform separate post-deploy health checks; deploy-web owns that verification and rollback.

You may learn new reusable skills with skill_manage; skill writes are approval-gated. Never rewrite this skill, SOUL.md, or .hermes.md unless the user explicitly asks to change the permanent operating rules.
`

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

func buildHermesInstallScriptBody() string {
	return `#!/bin/bash
set -Eeuo pipefail
export PATH="/root/.local/bin:/usr/local/bin:$PATH"

curl -fsSL https://hermes-agent.nousresearch.com/install.sh \
  | bash -s -- --skip-setup --skip-browser --skip-computer-use --no-skills

HERMES="$(command -v hermes)"
[ -n "$HERMES" ]
ln -sf "$HERMES" /usr/local/bin/hermes
hermes skills opt-out --remove --yes >/dev/null 2>&1 || hermes skills opt-out >/dev/null

hermes config set agent.disabled_toolsets '["browser","computer_use","code_execution","delegation","vision"]' >/dev/null
hermes config set agent.tool_use_enforcement true >/dev/null
hermes config set tool_output.max_bytes 30000 >/dev/null
hermes config set tool_output.max_lines 800 >/dev/null
hermes config set skills.write_approval true >/dev/null
hermes config set memory.memory_enabled true >/dev/null
hermes config set memory.user_profile_enabled true >/dev/null
hermes config set memory.write_approval false >/dev/null

mkdir -p /root/.hermes/skills/farzher-web-development
cat >/root/.hermes/SOUL.md <<'SOUL'
` + hermesSoul + `SOUL

cat >/root/.hermes/skills/farzher-web-development/SKILL.md <<'SKILL'
` + hermesWebDevelopmentSkill + `SKILL

hermes --version
`
}

func hermesManagedHash() string {
	return contentHash(buildHermesInstallScriptBody())
}

func buildHermesInstallScript() string {
	return buildHermesInstallScriptBody() + "printf '%s\\n' " + shellQuote(hermesManagedHash()) + " >" + shellQuote(hermesManagedHashFile) + "\n"
}

func installHermes(cfg config) (commandResult, error) {
	return runRemoteScript(cfg, 15*time.Minute, buildHermesInstallScript())
}

func ensureChatGPT(cfg config) (commandResult, error) {
	status, err := runTimeout(45*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--command=sudo -n -i hermes auth status openai-codex", "--quiet")
	if err != nil || !chatGPTLoggedIn(usefulOutput(status)) {
		return status, errChatGPTAuthRequired
	}
	remote := `sudo -n -i bash -lc 'set -e; hermes config set model.provider openai-codex >/dev/null; hermes config set model.default ` + chatGPTModel + ` >/dev/null; hermes config set agent.reasoning_effort ` + chatGPTEffort + ` >/dev/null; hermes config unset model.base_url >/dev/null 2>&1 || true'`
	configured, err := runTimeout(45*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--command="+remote, "--quiet")
	return mergeResult(status, configured), err
}

func chatGPTLoggedIn(output string) bool {
	x := strings.ToLower(output)
	if strings.Contains(x, "not logged") || strings.Contains(x, "not authenticated") || strings.Contains(x, "missing") {
		return false
	}
	return strings.Contains(x, "logged in") || strings.Contains(x, "authenticated")
}
