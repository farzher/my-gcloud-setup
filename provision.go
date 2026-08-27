package main

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

func scanExistingVMs(ctx context.Context, managedProject string) ([]existingVM, int) {
	r, err := run(ctx, "gcloud", "projects", "list", "--filter=lifecycleState:ACTIVE", "--format=value(projectId)")
	if err != nil {
		return nil, 0
	}
	projects := uniqueLines(r.Stdout)
	type result struct{ v []existingVM }
	out := make(chan result, len(projects))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, project := range projects {
		project := project
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			r, e := run(ctx, "gcloud", "compute", "instances", "list", "--project="+project, "--format=value(name,zone.basename(),status)")
			if e != nil {
				return
			}
			var found []existingVM
			for _, line := range nonEmptyLines(r.Stdout) {
				f := strings.Fields(line)
				if len(f) == 0 {
					continue
				}
				if project == managedProject && f[0] == vmName {
					continue
				}
				v := existingVM{Project: project, Name: f[0]}
				if len(f) > 1 {
					v.Zone = f[1]
				}
				if len(f) > 2 {
					v.Status = f[2]
				}
				found = append(found, v)
			}
			if len(found) > 0 {
				select {
				case out <- result{found}:
				case <-ctx.Done():
				}
			}
		}()
	}
	go func() { wg.Wait(); close(out) }()
	var all []existingVM
	for r := range out {
		all = append(all, r.v...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Project == all[j].Project {
			return all[i].Name < all[j].Name
		}
		return all[i].Project < all[j].Project
	})
	count := len(all)
	if len(all) > 3 {
		all = all[:3]
	}
	return all, count
}

func runStepCmd(index int, cfg config, billingID string) tea.Cmd {
	return func() tea.Msg {
		newCfg, detail, r, err := runProvisionStep(index, cfg, billingID)
		return stepDoneMsg{index, newCfg, detail, usefulOutput(r), r.Command, err}
	}
}

func runProvisionStep(index int, cfg config, billingID string) (config, string, commandResult, error) {
	var zero commandResult
	switch index {
	case 0:
		return ensureProject(cfg)
	case 1:
		if cfg.Project == "" || billingID == "" {
			return cfg, "", zero, errors.New("billing is not selected")
		}
		r, err := runTimeout(90*time.Second, "gcloud", "billing", "projects", "link", cfg.Project, "--billing-account="+billingID, "--quiet")
		return cfg, "", r, err
	case 2:
		r, err := runTimeout(3*time.Minute, "gcloud", "services", "enable", "compute.googleapis.com", "--project="+cfg.Project, "--quiet")
		return cfg, "", r, err
	case 3:
		r, err := ensureNetwork(cfg)
		return cfg, "", r, err
	case 4:
		r, ip, err := ensureAddress(cfg)
		return cfg, ip, r, err
	case 5:
		r, err := ensureVM(cfg)
		return cfg, "e2-micro", r, err
	case 6:
		r, err := waitForSSH(cfg)
		return cfg, "", r, err
	case 7:
		r, err := setupSystem(cfg)
		return cfg, "1 GB + 1 GB swap", r, err
	case 8:
		r, err := installHermes(cfg)
		return cfg, "lean", r, err
	case 9:
		r, err := ensureChatGPT(cfg)
		return cfg, chatGPTModel + " · " + chatGPTEffort, r, err
	case 10:
		newCfg, r, err := ensureGitHub(cfg)
		return newCfg, newCfg.Repo, r, err
	case 11:
		r, err := setupWeb(cfg)
		return cfg, "Node · Postgres · systemd · Nginx", r, err
	case 12:
		r, err := ensureDNS(cfg)
		return cfg, cfg.domainFor(cfg.Account), r, err
	case 13:
		r, err := ensureHTTPS(cfg)
		return cfg, cfg.domainFor(cfg.Account), r, err
	case 14:
		r, err := verifyServer(cfg)
		return cfg, "ready", r, err
	default:
		return cfg, "", zero, fmt.Errorf("unknown step %d", index)
	}
}

func ensureProject(cfg config) (config, string, commandResult, error) {
	if cfg.Project != "" {
		r, err := runTimeout(30*time.Second, "gcloud", "projects", "describe", cfg.Project, "--format=value(projectId)")
		if err == nil {
			labels, le := ensureProjectLabels(cfg.Project, cfg.Account)
			r = mergeResult(r, labels)
			if le != nil {
				return cfg, "", r, le
			}
			o, oe := ensureProjectOwner(cfg.Project)
			return cfg, cfg.Project, mergeResult(r, o), oe
		}
		if !looksNotFound(usefulOutput(r)) {
			return cfg, "", r, err
		}
		delete(cfg.Projects, cfg.Account)
		cfg.Project = ""
		if err = saveConfig(cfg); err != nil {
			return cfg, "", r, err
		}
	}
	name := cfg.nameFor(cfg.Account)
	if name == "" {
		name = "Cloud server"
	}
	label := accountHash(cfg.Account)
	var last commandResult
	var lastErr error
	for i := 0; i < 5; i++ {
		id := "cloud-" + randomHex(5)
		r, err := runTimeout(90*time.Second, "gcloud", "projects", "create", id, "--name="+name, "--labels=cloud-charm=managed,cloud_account="+label, "--quiet")
		last, lastErr = r, err
		if err == nil {
			cfg.setProject(cfg.Account, id)
			cfg.setDisabled(cfg.Account, false)
			if err = saveConfig(cfg); err != nil {
				return cfg, "", r, err
			}
			o, oe := ensureProjectOwner(id)
			r = mergeResult(r, o)
			if oe != nil {
				return cfg, "", r, oe
			}
			return cfg, id, r, nil
		}
	}
	return cfg, "", last, fmt.Errorf("project create failed: %w", lastErr)
}

func ensureProjectLabels(project, account string) (commandResult, error) {
	return runTimeout(45*time.Second, "gcloud", "projects", "update", project,
		"--update-labels=cloud-charm=managed,cloud_account="+accountHash(account), "--quiet")
}

func projectOwner(ctx context.Context, project, email string) (bool, error) {
	r, err := run(ctx, "gcloud", "projects", "get-iam-policy", project,
		"--flatten=bindings[].members", "--filter=bindings.role:roles/owner AND bindings.members:user:"+email, "--format=value(bindings.members)")
	return strings.Contains(r.Stdout, "user:"+email), err
}

func ensureProjectOwner(project string) (commandResult, error) {
	var last commandResult
	var err error
	for i := 0; i < 6; i++ {
		last, err = runTimeout(30*time.Second, "gcloud", "projects", "add-iam-policy-binding", project,
			"--member=user:"+adminEmail, "--role=roles/owner", "--condition=None", "--quiet")
		if err == nil {
			return last, nil
		}
		time.Sleep(2 * time.Second)
	}
	return last, err
}
