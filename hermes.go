package main

import "time"

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
    "terminal.cwd": "/website/app",
}
for path, value in settings.items():
    set_value(path, value)

set_value("model.provider", "openai-codex")
set_value("model.default", "` + chatGPTModel + `")
set_value("agent.reasoning_effort", "` + chatGPTEffort + `")
model = cfg.get("model")
if isinstance(model, dict):
    model.pop("base_url", None)

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

INSTALLER=""
MANAGED_SCRIPT="$(mktemp)"
trap 'rm -f "$MANAGED_SCRIPT"; if [ -n "${INSTALLER:-}" ]; then rm -f "$INSTALLER"; fi' EXIT

# Reuse a healthy install. Fresh or broken installs go through Hermes's
# supported noninteractive installer interface instead of private stage functions.
HERMES="$(command -v hermes || true)"
if [ -z "$HERMES" ] || [ ! -d /root/.hermes/hermes-agent/.git ] || ! "$HERMES" --version >/dev/null 2>&1; then
  INSTALLER="$(mktemp)"
  curl -fsSL https://hermes-agent.nousresearch.com/install.sh -o "$INSTALLER"
  bash "$INSTALLER"     --dir /root/.hermes/hermes-agent     --hermes-home /root/.hermes     --skip-setup     --skip-browser     --skip-computer-use     --no-skills     --non-interactive
  HERMES="$(command -v hermes || true)"
fi
[ -n "$HERMES" ]
"$HERMES" --version >/dev/null

# Run all managed configuration in one Hermes runtime startup. The temporary
# script stays outside the Hermes git checkout so startup/update checks never
# see our provisioning helper as an untracked working-tree change.
cat >"$MANAGED_SCRIPT" <<'PY'
` + buildHermesManagedConfigModule() + `PY
python3 - "$MANAGED_SCRIPT" <<'PY'
import os
import sys
from pathlib import Path

root = Path("/root/.hermes/hermes-agent")
sys.path.insert(0, str(root))
from hermes_cli._launchers import runtime_command

script = sys.argv[1]
code = "exec(compile(open(" + repr(script) + ", 'rb').read(), " + repr(script) + ", 'exec'))"
cmd = runtime_command(root, code=code)
os.execv(cmd[0], cmd)
PY
`
}
func hermesManagedHash() string {
	return contentHash(buildHermesInstallScriptBody())
}

func chatGPTManagedHash() string {
	return contentHash("openai-codex\n" + chatGPTModel + "\n" + chatGPTEffort + "\n")
}

func buildHermesInstallScript() string {
	return buildHermesInstallScriptBody() +
		"printf '%s\\n' " + shellQuote(hermesManagedHash()) + " >" + shellQuote(hermesManagedHashFile) + "\n" +
		"printf '%s\\n' " + shellQuote(chatGPTManagedHash()) + " >" + shellQuote(chatGPTManagedHashFile) + "\n"
}

func installHermes(cfg config) (commandResult, error) {
	return runRemoteScript(cfg, 30*time.Minute, buildHermesInstallScript())
}

func ensureChatGPT(cfg config) (commandResult, error) {
	loggedIn, err := chatGPTAuthStatus(cfg.Project, cfg.zone())
	if err != nil {
		return commandResult{}, err
	}
	if !loggedIn {
		return commandResult{}, errChatGPTAuthRequired
	}
	return commandResult{}, nil
}
