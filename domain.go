package main

import (
	"context"
	"errors"
	"os"
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
	ipResult, err := runTimeout(30*time.Second, "gcloud", "compute", "addresses", "describe", addressName, "--project="+cfg.Project, "--region="+cfg.region(), "--format=value(address)")
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
	hermesHash := hermesManagedHash()
	deployHash := contentHash(buildDeployScript())
	shipHash := contentHash(buildShipScript())
	statusHash := contentHash(buildStatusScript())
	backupHash := contentHash(buildBackupScript())
	restoreHash := contentHash(buildRestoreScript())
	contextHash := contentHash(buildHermesProjectContext(cfg, domain))
	script := `set -Eeuo pipefail
. /etc/os-release
[ "$ID" = debian ] && [ "${VERSION_ID%%.*}" = 13 ]
swapon --show=NAME --noheadings | grep -qx /swapfile
[ "$(stat -c %s /swapfile)" = 1073741824 ]
command -v node >/dev/null
command -v psql >/dev/null
command -v nginx >/dev/null
command -v hermes >/dev/null
hermes auth status openai-codex | grep -Eqi 'logged in|authenticated'
[ "$(hermes config get model.provider)" = openai-codex ]
[ "$(hermes config get model.default)" = ` + chatGPTModel + ` ]
[ "$(hermes config get agent.reasoning_effort)" = ` + chatGPTEffort + ` ]
[ -s /root/.hermes/SOUL.md ]
[ "$(cat ` + shellQuote(hermesManagedHashFile) + `)" = "` + hermesHash + `" ]
[ ! -d /website/.git ]
[ -d /website/app/.git ]
[ "$(git -C /website/app remote get-url origin)" = ` + shellQuote("git@github.com:"+cfg.Repo+".git") + ` ]
grep -qxF ` + shellQuote(githubKnownHost) + ` /root/.ssh/known_hosts
[ -d /website/data ]
[ -f /var/lib/website/initialized ]
[ -s /website/app/AGENTS.md ]
[ -x /website/app/ops/deploy.sh ]
[ -x /website/app/ops/ship.sh ]
[ -x /website/app/ops/status.sh ]
[ -x /website/app/ops/backup.sh ]
[ -x /website/app/ops/restore.sh ]
[ "$(sha256sum /website/app/ops/deploy.sh | awk '{print $1}')" = "` + deployHash + `" ]
[ "$(sha256sum /website/app/ops/ship.sh | awk '{print $1}')" = "` + shipHash + `" ]
[ "$(sha256sum /website/app/ops/status.sh | awk '{print $1}')" = "` + statusHash + `" ]
[ "$(sha256sum /website/app/ops/backup.sh | awk '{print $1}')" = "` + backupHash + `" ]
[ "$(sha256sum /website/app/ops/restore.sh | awk '{print $1}')" = "` + restoreHash + `" ]
[ "$(sha256sum /website/app/AGENTS.md | awk '{print $1}')" = "` + contextHash + `" ]
[ -x /usr/local/bin/deploy-web ]
[ -x /usr/local/bin/ship-web ]
[ -x /usr/local/bin/server-status ]
[ -x /usr/local/bin/backup-web ]
[ -x /usr/local/bin/restore-web ]
systemctl is-enabled --quiet web.service
systemctl is-active --quiet web.service
systemctl is-enabled --quiet web-backup.timer
systemctl is-active --quiet web-backup.timer
systemctl is-active --quiet nginx
systemctl is-active --quiet postgresql
nginx -t >/dev/null 2>&1
nginx -T 2>/dev/null | grep -Fq 'proxy_pass http://127.0.0.1:3000;'
/usr/local/bin/server-status >/dev/null
`
	if domain != "" {
		cert := "/etc/letsencrypt/live/" + domain + "/fullchain.pem"
		script += `[ -s ` + shellQuote(cert) + ` ]
nginx -T 2>/dev/null | grep -Fq ` + shellQuote("ssl_certificate "+cert) + `
`
	}
	script += `printf 'ready\n'
`
	return runRemoteBash(cfg, 60*time.Second, script)
}

func runRemoteScript(cfg config, timeout time.Duration, script string) (commandResult, error) {
	f, err := os.CreateTemp(".", ".cloud-script-*.sh")
	if err != nil {
		return commandResult{}, err
	}
	local := f.Name()
	defer os.Remove(local)
	if _, err = f.WriteString(script); err != nil {
		_ = f.Close()
		return commandResult{}, err
	}
	if err = f.Close(); err != nil {
		return commandResult{}, err
	}

	remote := "/tmp/cloud-script-" + randomHex(10) + ".sh"
	copied, err := runTimeout(2*time.Minute, "gcloud", "compute", "scp", local, vmName+":"+remote,
		"--project="+cfg.Project, "--zone="+cfg.zone(), "--quiet")
	if err != nil {
		return copied, err
	}

	command := "sudo -n bash " + shellQuote(remote) + "; code=$?; rm -f " + shellQuote(remote) + "; exit $code"
	executed, err := runTimeout(timeout, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+cfg.zone(), "--command="+command, "--quiet")
	return mergeResult(copied, executed), err
}

func runRemoteBash(cfg config, timeout time.Duration, script string) (commandResult, error) {
	cmd := "sudo -n bash -c " + shellQuote(script)
	return runTimeout(timeout, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+cfg.zone(), "--command="+cmd, "--quiet")
}
