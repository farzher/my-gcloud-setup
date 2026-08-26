package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

func detect(cfg config) (cloudState, error) {
	var s cloudState
	if !hasExecutable("gcloud") {
		return s, nil
	}
	s.Gcloud = true
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if r, err := run(ctx, "gcloud", "auth", "list", "--format=value(account)"); err == nil {
		s.Accounts = uniqueLines(r.Stdout)
		sort.Strings(s.Accounts)
	}
	r, err := run(ctx, "gcloud", "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
	if err != nil {
		return s, fmt.Errorf("gcloud auth: %w\n%s", err, usefulOutput(r))
	}
	s.Account = firstLine(r.Stdout)
	if s.Account == "" {
		return s, nil
	}

	if r, err = run(ctx, "gcloud", "billing", "accounts", "list", "--filter=open=true", "--format=json"); err == nil && r.Stdout != "" {
		_ = json.Unmarshal([]byte(r.Stdout), &s.Billing)
	}

	project := cfg.projectFor(s.Account)
	if project == "" {
		s.ManagedProject = discoverManagedProject(ctx, s.Account)
		project = s.ManagedProject
	}
	if project == "" {
		return s, nil
	}

	if r, err = run(ctx, "gcloud", "projects", "describe", project, "--format=json"); err != nil {
		return s, nil
	}
	var p struct {
		ProjectID string            `json:"projectId"`
		Name      string            `json:"name"`
		Labels    map[string]string `json:"labels"`
	}
	_ = json.Unmarshal([]byte(r.Stdout), &p)
	s.ProjectOK, s.ProjectName = p.ProjectID != "", p.Name
	if s.ProjectOK {
		if p.Labels["cloud-charm"] != "managed" || p.Labels["cloud_account"] != accountHash(s.Account) {
			if _, labelErr := ensureProjectLabels(project, s.Account); labelErr != nil {
				return s, fmt.Errorf("project labels: %w", labelErr)
			}
		}
		owner, ownerErr := projectOwner(ctx, project, adminEmail)
		if ownerErr != nil {
			return s, fmt.Errorf("owner check: %w", ownerErr)
		}
		if !owner {
			if _, ownerErr = ensureProjectOwner(project); ownerErr != nil {
				return s, fmt.Errorf("owner setup: %w", ownerErr)
			}
		}
	}

	if r, err = run(ctx, "gcloud", "compute", "instances", "describe", vmName, "--project="+project, "--zone="+zone, "--format=json"); err == nil && r.Stdout != "" {
		s.VMExists = true
		_ = json.Unmarshal([]byte(r.Stdout), &s.Instance)
	}
	if r, err = run(ctx, "gcloud", "compute", "addresses", "describe", addressName, "--project="+project, "--region="+region, "--format=value(address)"); err == nil {
		s.StaticIP = firstLine(r.Stdout)
	}
	if s.VMExists && strings.EqualFold(s.Instance.Status, "RUNNING") {
		probe, _ := runTimeout(35*time.Second, "gcloud", "compute", "ssh", vmName,
			"--project="+project, "--zone="+zone, "--command="+remoteProbe(cfg, s.StaticIP), "--quiet")
		for _, line := range nonEmptyLines(probe.Stdout) {
			switch strings.TrimSpace(line) {
			case "READY_SSH":
				s.SSHReady = true
			case "READY_SYSTEM":
				s.SystemReady = true
			case "READY_HERMES":
				s.HermesReady = true
			case "READY_CHATGPT":
				s.ChatGPTReady = true
			case "READY_GITHUB":
				s.GitHubReady = true
			case "READY_WEB":
				s.WebReady = true
			case "READY_DNS":
				s.DNSReady = true
			case "READY_HTTPS":
				s.HTTPSReady = true
			case "READY_ALL":
				s.VerifyReady = true
			}
		}
		domainOK := cfg.domainFor(s.Account) == "" || (s.DNSReady && s.HTTPSReady)
		s.VerifyReady = s.SSHReady && s.SystemReady && s.HermesReady && s.ChatGPTReady && s.GitHubReady && s.WebReady && domainOK
	}
	return s, nil
}

func remoteProbe(cfg config, staticIP string) string {
	domain := cfg.domainFor(cfg.Account)
	script := `
echo READY_SSH
if command -v node >/dev/null && command -v psql >/dev/null && command -v pm2 >/dev/null && command -v nginx >/dev/null && swapon --show=NAME --noheadings | grep -qx /swapfile; then echo READY_SYSTEM; fi
if command -v hermes >/dev/null && [ -s /root/.hermes/SOUL.md ] && [ -s /root/.hermes/skills/farzher-web-development/SKILL.md ]; then echo READY_HERMES; fi
if command -v hermes >/dev/null && hermes auth status openai-codex 2>/dev/null | grep -Eqi "logged in|authenticated" && [ "$(hermes config get model.provider 2>/dev/null)" = "openai-codex" ] && [ "$(hermes config get model.default 2>/dev/null)" = "` + chatGPTModel + `" ] && [ "$(hermes config get agent.reasoning_effort 2>/dev/null)" = "` + chatGPTEffort + `" ]; then echo READY_CHATGPT; fi
if [ -d /website/.git ] && git -C /website remote get-url origin >/dev/null 2>&1; then echo READY_GITHUB; fi
if [ -d /website/data ] && grep -qxF 'data/' /website/.gitignore && [ -x /website/ops/deploy.sh ] && [ -x /website/ops/backup.sh ] && [ -x /website/ops/restore.sh ] && [ -s /website/.hermes.md ] && [ -f /var/lib/website/state-initialized ] && [ -x /usr/local/bin/backup-web ] && [ -x /usr/local/bin/restore-web ] && systemctl is-enabled --quiet web-backup.timer && systemctl is-active --quiet web-backup.timer && systemctl is-active --quiet nginx && systemctl is-active --quiet postgresql && [ -s /var/lib/website/current-slot ]; then echo READY_WEB; fi
`
	if domain == "" {
		script += "echo READY_DNS\necho READY_HTTPS\n"
	} else {
		script += `if getent ahostsv4 ` + shellQuote(domain) + ` 2>/dev/null | awk '{print $1}' | grep -Fxq ` + shellQuote(staticIP) + `; then echo READY_DNS; fi
`
		script += `if [ -s /etc/letsencrypt/live/` + domain + `/fullchain.pem ]; then echo READY_HTTPS; fi
`
	}
	return "sudo -n bash -c " + shellQuote(script)
}

func discoverManagedProject(ctx context.Context, account string) string {
	hash := accountHash(account)
	r, err := run(ctx, "gcloud", "projects", "list", "--filter=labels.cloud-charm=managed AND labels.cloud_account="+hash, "--format=value(projectId)")
	if err != nil {
		return ""
	}
	items := uniqueLines(r.Stdout)
	if len(items) == 1 {
		return items[0]
	}
	return ""
}

func scanVMsCmd(account, managedProject string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		vms, count := scanExistingVMs(ctx, managedProject)
		return vmScanMsg{account, vms, count}
	}
}
