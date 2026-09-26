package main

import (
	"strings"
	"time"
)

// This file defines managed Hermes defaults and generated project rules.
// SOUL.md is a bootstrap default; evolved SOUL, memory, and skills are preserved by backups.

const (
	chatGPTModel           = "gpt-5.6-sol"
	chatGPTEffort          = "medium"
	hermesManagedHashFile  = "/root/.hermes/.managed-config-hash"
	chatGPTManagedHashFile = "/root/.hermes/.managed-chatgpt-hash"
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
- Site-specific Nginx rules: /website/app/ops/nginx.conf
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
- Implement requested changes directly and minimally; do not refactor unrelated code.
- Keep commands serial and lightweight. Do not use browsers, computer-use, subagents, containers, or heavyweight tooling unless explicitly requested.
- Do not add or run tests, linters, type checks, benchmarks, or manual health checks unless explicitly requested; deploy-web performs the deployment health check.
- Put durable file bytes in DATA_DIR and structured/queryable state in PostgreSQL. Never put runtime data in the app repository.
- Tracked static assets belong in the app repo; mutable, generated, or user-created files belong under DATA_DIR and should be served from there rather than copied into the repo.
- Before shipping, remove discarded static-asset variants that are no longer referenced or intentionally retained.
- Treat production performance as a standing requirement: keep ops/nginx.conf appropriately optimized for the site's actual architecture and content whenever relevant, without allowing stale deployments, while preserving correct application behavior and /healthz.
- Keep GET /healthz lightweight and unauthenticated; return 200 only when the web app and PostgreSQL are healthy, and report both statuses in the response.
- After code changes, run ship-web "<short commit message>", then reply when it succeeds. deploy-web validates and reloads Nginx before restarting/checking Node.
- If ship-web or deploy-web fails because of the code or dependencies you changed, diagnose the failure, fix it, and retry. Do not stop at the first self-caused failure.
- If the failure is external or infrastructural (for example auth, permissions, disk, database service, network, or an operation lock), or cannot be safely fixed from the requested change, report it instead of making unrelated server changes.
- Backups are automatic. Run backup-web when explicitly asked for a snapshot; restore only when explicitly asked.
- These permanent rules override MEMORY.md and are changed only when the user explicitly asks.
`
	return context
}

func buildHermesManagedConfigModule() string {
	return `from hermes_cli.config import read_raw_config, save_config
from tools.skills_sync_bundled_ops import remove_pristine_bundled_skills

cfg = read_raw_config()

def set_value(path, value):
    node = cfg
    parts = path.split(".")
    for part in parts[:-1]:
        child = node.get(part)
        if not isinstance(child, dict):
            child = {}
            node[part] = child
        node = child
    node[parts[-1]] = value

settings = {
    "agent.disabled_toolsets": ["browser", "computer_use", "code_execution", "delegation", "vision"],
    "agent.tool_use_enforcement": True,
    "tool_output.max_bytes": 30000,
    "tool_output.max_lines": 800,
    "approvals.mode": "off",
    "approvals.cron_mode": "approve",
    "approvals.single_query_mode": "approve",
    "approvals.mcp_reload_confirm": False,
    "approvals.destructive_slash_confirm": False,
    "skills.write_approval": False,
    "memory.memory_enabled": True,
    "memory.user_profile_enabled": True,
    "memory.write_approval": False,
    "sessions.auto_prune": True,
    "sessions.retention_days": 30,
    "sessions.vacuum_after_prune": True,
    "sessions.min_vacuum_interval_days": 30,
    "sessions.min_interval_hours": 24,
    "gateway.write_sessions_json": False,
}
for path, value in settings.items():
    set_value(path, value)

save_config(cfg, merge_existing=False)
remove_pristine_bundled_skills(dry_run=False)
`
}

func buildChatGPTConfigModule() string {
	return `from hermes_cli.config import read_raw_config, save_config

cfg = read_raw_config()

def set_value(path, value):
    node = cfg
    parts = path.split(".")
    for part in parts[:-1]:
        child = node.get(part)
        if not isinstance(child, dict):
            child = {}
            node[part] = child
        node = child
    node[parts[-1]] = value

def unset_value(path):
    node = cfg
    parts = path.split(".")
    for part in parts[:-1]:
        node = node.get(part)
        if not isinstance(node, dict):
            return
    node.pop(parts[-1], None)

set_value("model.provider", "openai-codex")
set_value("model.default", "` + chatGPTModel + `")
set_value("agent.reasoning_effort", "` + chatGPTEffort + `")
unset_value("model.base_url")
save_config(cfg, merge_existing=False)
`
}

func chatGPTAuthProbePython() string {
	return `import json, sys
from pathlib import Path
try:
    data = json.loads(Path("/root/.hermes/auth.json").read_text(encoding="utf-8-sig"))
except Exception:
    raise SystemExit(1)
providers = data.get("providers") if isinstance(data.get("providers"), dict) else {}
entry = providers.get("openai-codex") if isinstance(providers.get("openai-codex"), dict) else {}
tokens = entry.get("tokens") if isinstance(entry.get("tokens"), dict) else {}
pool = data.get("credential_pool") if isinstance(data.get("credential_pool"), dict) else {}
rows = pool.get("openai-codex") if isinstance(pool.get("openai-codex"), list) else []
logged_in = bool(tokens.get("access_token")) or any(
    isinstance(row, dict) and row.get("access_token") for row in rows
)
raise SystemExit(0 if logged_in else 1)
`
}

func buildHermesInstallScriptBody() string {
	return `#!/bin/bash
set -Eeuo pipefail
export PATH="/root/.local/bin:/usr/local/bin:$PATH"

# Keep native Python builds conservative on the 1 GB VM. Browser tooling is
# intentionally omitted on this headless server.
export UV_CONCURRENT_BUILDS=1
export UV_CONCURRENT_INSTALLS=1
export MAKEFLAGS="-j1"
export CMAKE_BUILD_PARALLEL_LEVEL=1
export CARGO_BUILD_JOBS=1

mkdir -p /root/.hermes
touch /root/.hermes/.no-bundled-skills
if [ ! -s /root/.hermes/SOUL.md ]; then
cat >/root/.hermes/SOUL.md <<'SOUL'
` + hermesSoul + `SOUL
fi

MANAGED_MODULE=/root/.hermes/hermes-agent/_cloud_managed_config.py
INSTALLER=""
trap 'rm -f "$MANAGED_MODULE"; if [ -n "${INSTALLER:-}" ]; then rm -f "$INSTALLER"; fi' EXIT

# A managed-config hash change means our settings changed, not Hermes itself.
# Reuse any published Hermes command and install only when it is absent.
HERMES="$(command -v hermes || true)"
if [ -z "$HERMES" ] || [ ! -d /root/.hermes/hermes-agent/.git ] || [ ! -x /root/.hermes/hermes-agent/.hermes/bin/hermes ]; then
  INSTALLER="$(mktemp)"
  curl -fsSL https://hermes-agent.nousresearch.com/install.sh -o "$INSTALLER"

  # Keep the server install lean. Do not run upstream's products stage: it
  # builds TUI/web products that this headless 1 GB server does not need.
  for stage in repository venv python-deps config; do
    bash "$INSTALLER" --stage "$stage" --skip-browser
  done

  # Publish only the command launchers, then mark the bootstrap complete.
  python3 /root/.hermes/hermes-agent/hermes_cli/_launchers.py /root/.local/bin
  bash "$INSTALLER" --stage complete --skip-browser
  HERMES="$(command -v hermes)"
fi
[ -n "$HERMES" ]

# One Hermes startup applies every managed setting. Starting Hermes repeatedly
# is extremely expensive on e2-micro because each launch performs source/runtime
# bookkeeping, so never shell out once per config key.
cat >"$MANAGED_MODULE" <<'PY'
` + buildHermesManagedConfigModule() + `PY
hermes --run-module _cloud_managed_config >/dev/null
rm -f "$MANAGED_MODULE"
`
}

func hermesManagedHash() string {
	return contentHash(buildHermesInstallScriptBody())
}

func chatGPTManagedHash() string {
	return contentHash("openai-codex\n" + chatGPTModel + "\n" + chatGPTEffort + "\n")
}

func buildHermesInstallScript() string {
	return buildHermesInstallScriptBody() + "printf '%s\\n' " + shellQuote(hermesManagedHash()) + " >" + shellQuote(hermesManagedHashFile) + "\n"
}

func installHermes(cfg config) (commandResult, error) {
	return runRemoteScript(cfg, 30*time.Minute, buildHermesInstallScript())
}

func configureChatGPT(project, zone string) (commandResult, error) {
	module := "/root/.hermes/hermes-agent/_cloud_chatgpt_config.py"
	script := `set -Eeuo pipefail
export PATH="/root/.local/bin:/usr/local/bin:$PATH"
MODULE=` + shellQuote(module) + `
trap 'rm -f "$MODULE"' EXIT
cat >"$MODULE" <<'PY'
` + buildChatGPTConfigModule() + `PY
hermes --run-module _cloud_chatgpt_config >/dev/null
printf '%s\\n' ` + shellQuote(chatGPTManagedHash()) + ` >` + shellQuote(chatGPTManagedHashFile) + `
`
	remote := "sudo -n -i bash -lc " + shellQuote(script)
	return runTimeout(90*time.Second, "gcloud", "compute", "ssh", vmName,
		"--project="+project, "--zone="+zone, "--command="+remote, "--quiet")
}

func ensureChatGPT(cfg config) (commandResult, error) {
	loggedIn, err := chatGPTAuthStatus(cfg.Project, cfg.zone())
	if err != nil {
		return commandResult{}, err
	}
	if !loggedIn {
		return commandResult{}, errChatGPTAuthRequired
	}
	return configureChatGPT(cfg.Project, cfg.zone())
}

func chatGPTLoggedIn(output string) bool {
	x := strings.ToLower(output)
	if strings.Contains(x, "not logged") || strings.Contains(x, "not authenticated") || strings.Contains(x, "missing") {
		return false
	}
	return strings.Contains(x, "logged in") || strings.Contains(x, "authenticated")
}
