package main

import (
	"strings"
	"time"
)

// This file is the single source of truth for all non-default Hermes setup.
// Edit it to change the model, Hermes settings, SOUL.md, or /website/app/.hermes.md.

const (
	chatGPTModel          = "gpt-5.6-sol"
	chatGPTEffort         = "medium"
	hermesManagedHashFile = "/root/.hermes/.managed-config-hash"
)

const hermesSoul = `You are Farzher's production web developer.
Be direct, fast, practical, and concise. Prefer the smallest straightforward implementation that fully solves the request.
`

func buildHermesProjectContext(cfg config, domain string) string {
	context := `# Production website

- App repository: /website/app (` + cfg.Repo + `)
- Persistent files: /website/data via DATA_DIR
- Structured state: PostgreSQL database web
- Runtime: Node.js + systemd + Nginx on a 1 GB VM
- Ship changes: /usr/local/bin/ship-web
- Deploy current checkout: /usr/local/bin/deploy-web
- Status: /usr/local/bin/server-status
- Backup: /usr/local/bin/backup-web
- Restore: /usr/local/bin/restore-web
`
	if domain != "" {
		context += "- Domain: " + domain + "\n"
	}
	context += `
## Rules
- There are no existing users or compatibility requirements. Do not add versioning, migrations, fallback behavior, compatibility shims, or legacy support unless explicitly requested; prefer the cleanest current design.
- Implement requested changes directly and minimally; do not refactor unrelated code.
- Keep commands serial and lightweight. Do not use browsers, computer-use, subagents, containers, or heavyweight tooling unless explicitly requested.
- Do not add or run tests, linters, type checks, benchmarks, or manual health checks unless explicitly requested; deploy-web performs the deployment health check.
- Put durable file bytes in DATA_DIR and structured/queryable state in PostgreSQL. Never put runtime data in the app repository.
- Tracked static assets belong in the app repo; mutable, generated, or user-created files belong under DATA_DIR and should be served from there rather than copied into the repo.
- Keep GET /healthz lightweight and unauthenticated; return 200 only when the web app and PostgreSQL are healthy, and report both statuses in the response.
- After code changes, run ship-web "<short commit message>", then reply when it succeeds.
- If ship-web or deploy-web fails because of the code or dependencies you changed, diagnose the failure, fix it, and retry. Do not stop at the first self-caused failure.
- If the failure is external or infrastructural (for example auth, permissions, disk, database service, network, or an operation lock), or cannot be safely fixed from the requested change, report it instead of making unrelated server changes.
- Backups are automatic. Run backup-web when explicitly asked for a snapshot; restore only when explicitly asked.
- These permanent rules override MEMORY.md and are changed only when the user explicitly asks.
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
hermes config set skills.write_approval false >/dev/null
hermes config set memory.memory_enabled true >/dev/null
hermes config set memory.user_profile_enabled true >/dev/null
hermes config set memory.write_approval false >/dev/null
hermes config set sessions.auto_prune true >/dev/null
hermes config set sessions.retention_days 30 >/dev/null
hermes config set sessions.vacuum_after_prune true >/dev/null
hermes config set sessions.min_vacuum_interval_days 30 >/dev/null
hermes config set sessions.min_interval_hours 24 >/dev/null
hermes config set gateway.write_sessions_json false >/dev/null

mkdir -p /root/.hermes
cat >/root/.hermes/SOUL.md <<'SOUL'
` + hermesSoul + `SOUL

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
