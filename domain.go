package main

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

func ensureDNS(cfg config) (commandResult, error) {
	domain := cfg.domainFor(cfg.Account)
	if domain == "" {
		return commandResult{}, nil
	}
	if cfg.Project == "" {
		return commandResult{}, errors.New("project is not set")
	}
	ipResult, err := runTimeout(30*time.Second, "gcloud", "compute", "addresses", "describe", addressName, "--project="+cfg.Project, "--region="+region, "--format=value(address)")
	if err != nil {
		return ipResult, err
	}
	ip := firstLine(ipResult.Stdout)
	if ip == "" {
		return ipResult, errors.New("static IP is missing")
	}
	script := `getent ahostsv4 ` + shellQuote(domain) + ` 2>/dev/null | awk '{print $1}' | grep -Fxq ` + shellQuote(ip)
	r, err := runRemoteBash(cfg, 30*time.Second, script)
	if err != nil {
		r.Stdout = strings.TrimSpace(r.Stdout + "\nSet A " + domain + " -> " + ip)
		return mergeResult(ipResult, r), errDNSRequired
	}
	return mergeResult(ipResult, r), nil
}

func ensureHTTPS(cfg config) (commandResult, error) {
	domain := cfg.domainFor(cfg.Account)
	if domain == "" {
		return commandResult{}, nil
	}
	script := `set -Eeuo pipefail
certbot --nginx --non-interactive --agree-tos --redirect --no-eff-email --email ` + shellQuote(adminEmail) + ` -d ` + shellQuote(domain) + `
systemctl enable --now certbot.timer >/dev/null 2>&1 || true
`
	return runRemoteBash(cfg, 5*time.Minute, script)
}

func verifyServer(cfg config) (commandResult, error) {
	domain := cfg.domainFor(cfg.Account)
	script := `set -Eeuo pipefail
. /etc/os-release
[ "$ID" = debian ] && [ "${VERSION_ID%%.*}" = 13 ]
swapon --show=NAME --noheadings | grep -qx /swapfile
[ "$(stat -c %s /swapfile)" = 1073741824 ]
command -v node >/dev/null
command -v psql >/dev/null
command -v pm2 >/dev/null
command -v nginx >/dev/null
command -v hermes >/dev/null
hermes auth status openai-codex | grep -Eqi 'logged in|authenticated'
[ "$(hermes config get model.provider)" = openai-codex ]
[ "$(hermes config get model.default)" = ` + chatGPTModel + ` ]
[ "$(hermes config get agent.reasoning_effort)" = ` + chatGPTEffort + ` ]
[ -s /root/.hermes/SOUL.md ]
[ -s /root/.hermes/skills/farzher-web-development/SKILL.md ]
grep -q '^## Persistent application files$' /root/.hermes/skills/farzher-web-development/SKILL.md
[ -d /website/.git ]
[ -d /website/data ]
grep -qxF 'data/' /website/.gitignore
[ -f /var/lib/website/state-initialized ]
[ -s /website/.hermes.md ]
[ -x /website/ops/deploy.sh ]
[ -x /website/ops/backup.sh ]
[ -x /website/ops/restore.sh ]
[ -x /usr/local/bin/backup-web ]
[ -x /usr/local/bin/restore-web ]
systemctl is-enabled --quiet web-backup.timer
systemctl is-active --quiet web-backup.timer
systemctl is-active --quiet nginx
systemctl is-active --quiet postgresql
SLOT="$(cat /var/lib/website/current-slot)"
pm2 describe "web-$SLOT" >/dev/null
`
	if domain != "" {
		script += `[ -s /etc/letsencrypt/live/` + domain + `/fullchain.pem ]
`
	}
	script += `printf 'ready\n'
`
	return runRemoteBash(cfg, 60*time.Second, script)
}

func runRemoteScript(cfg config, timeout time.Duration, script string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--command=sudo -n bash -s", "--quiet")
	cmd.Stdin = strings.NewReader(script)
	return runExec(cmd)
}

func runRemoteBash(cfg config, timeout time.Duration, script string) (commandResult, error) {
	cmd := "sudo -n bash -c " + shellQuote(script)
	return runTimeout(timeout, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--command="+cmd, "--quiet")
}
