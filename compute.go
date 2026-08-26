package main

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func ensureNetwork(cfg config) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var all commandResult
	if !gcloudExists(ctx, "compute", "networks", "describe", networkName, "--project="+cfg.Project) {
		r, e := run(ctx, "gcloud", "compute", "networks", "create", networkName, "--project="+cfg.Project, "--subnet-mode=custom", "--quiet")
		all = mergeResult(all, r)
		if e != nil {
			return all, e
		}
	}
	if !gcloudExists(ctx, "compute", "networks", "subnets", "describe", subnetName, "--project="+cfg.Project, "--region="+region) {
		r, e := run(ctx, "gcloud", "compute", "networks", "subnets", "create", subnetName, "--project="+cfg.Project, "--region="+region, "--network="+networkName, "--range=10.10.0.0/24", "--quiet")
		all = mergeResult(all, r)
		if e != nil {
			return all, e
		}
	}
	if !gcloudExists(ctx, "compute", "firewall-rules", "describe", firewall, "--project="+cfg.Project) {
		r, e := run(ctx, "gcloud", "compute", "firewall-rules", "create", firewall, "--project="+cfg.Project, "--network="+networkName,
			"--direction=INGRESS", "--allow=tcp:22,tcp:80,tcp:443", "--source-ranges=0.0.0.0/0", "--target-tags="+networkTag, "--quiet")
		all = mergeResult(all, r)
		if e != nil {
			return all, e
		}
	}
	return all, nil
}

func ensureAddress(cfg config) (commandResult, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var all commandResult
	if !gcloudExists(ctx, "compute", "addresses", "describe", addressName, "--project="+cfg.Project, "--region="+region) {
		r, e := run(ctx, "gcloud", "compute", "addresses", "create", addressName, "--project="+cfg.Project, "--region="+region, "--network-tier=PREMIUM", "--quiet")
		all = mergeResult(all, r)
		if e != nil {
			return all, "", e
		}
	}
	r, e := run(ctx, "gcloud", "compute", "addresses", "describe", addressName, "--project="+cfg.Project, "--region="+region, "--format=value(address)")
	all = mergeResult(all, r)
	return all, firstLine(r.Stdout), e
}

func ensureVM(cfg config) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if gcloudExists(ctx, "compute", "instances", "describe", vmName, "--project="+cfg.Project, "--zone="+zone) {
		return run(ctx, "gcloud", "compute", "instances", "describe", vmName, "--project="+cfg.Project, "--zone="+zone, "--format=value(status)")
	}
	return run(ctx, "gcloud", "compute", "instances", "create", vmName,
		"--project="+cfg.Project, "--zone="+zone, "--machine-type=e2-micro", "--image-family=debian-13", "--image-project=debian-cloud",
		"--boot-disk-size=30GB", "--boot-disk-type=pd-standard", "--subnet="+subnetName, "--address="+addressName, "--network-tier=PREMIUM",
		"--tags="+networkTag, "--no-service-account", "--no-scopes", "--no-deletion-protection", "--quiet")
}

func waitForSSH(cfg config) (commandResult, error) {
	var last commandResult
	var err error
	for i := 0; i < 30; i++ {
		last, err = runTimeout(15*time.Second, "gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+zone, "--command=echo CLOUD_OK", "--quiet")
		if err == nil && strings.Contains(last.Stdout, "CLOUD_OK") {
			return last, nil
		}
		time.Sleep(3 * time.Second)
	}
	if err == nil {
		err = errors.New("SSH not ready")
	}
	return last, err
}

func renameSiteCmd(cfg config, name, domain string) tea.Cmd {
	return func() tea.Msg {
		r, err := runTimeout(60*time.Second, "gcloud", "projects", "update", cfg.Project, "--name="+name, "--quiet")
		if err == nil {
			cfg.setSite(cfg.Account, name, domain)
			err = saveConfig(cfg)
			if err == nil {
				web, webErr := setupWeb(cfg)
				r = mergeResult(r, web)
				err = webErr
			}
		}
		return renameDoneMsg{cfg, usefulOutput(r), err}
	}
}

func lifecycleCmd(name string, cfg config, action string) tea.Cmd {
	return func() tea.Msg {
		r, e := runTimeout(3*time.Minute, "gcloud", "compute", "instances", action, vmName, "--project="+cfg.Project, "--zone="+zone, "--quiet")
		return actionDoneMsg{name, cfg, usefulOutput(r), e}
	}
}

func rebuildCmd(cfg config, billingID string) tea.Cmd {
	return func() tea.Msg {
		var all commandResult
		statusResult, err := runTimeout(30*time.Second, "gcloud", "compute", "instances", "describe", vmName,
			"--project="+cfg.Project, "--zone="+zone, "--format=value(status)")
		all = mergeResult(all, statusResult)
		if err != nil {
			if looksNotFound(usefulOutput(statusResult)) {
				return rebuildReadyMsg{cfg, billingID}
			}
			return actionDoneMsg{"Rebuild", cfg, usefulOutput(all), err}
		}

		status := strings.ToUpper(firstLine(statusResult.Stdout))
		started := false
		switch status {
		case "RUNNING":
		case "TERMINATED", "STOPPED":
			start, startErr := runTimeout(3*time.Minute, "gcloud", "compute", "instances", "start", vmName,
				"--project="+cfg.Project, "--zone="+zone, "--quiet")
			all = mergeResult(all, start)
			if startErr != nil {
				return actionDoneMsg{"Rebuild", cfg, usefulOutput(all), startErr}
			}
			started = true
			ssh, sshErr := waitForSSH(cfg)
			all = mergeResult(all, ssh)
			if sshErr != nil {
				_, _ = runTimeout(3*time.Minute, "gcloud", "compute", "instances", "stop", vmName, "--project="+cfg.Project, "--zone="+zone, "--quiet")
				return actionDoneMsg{"Rebuild", cfg, usefulOutput(all), fmt.Errorf("cannot back up before rebuild: %w", sshErr)}
			}
		default:
			return actionDoneMsg{"Rebuild", cfg, usefulOutput(all), fmt.Errorf("VM is %s; retry rebuild when it is running or stopped", status)}
		}

		backup, backupErr := runRemoteBash(cfg, 10*time.Minute, "/usr/local/bin/backup-web")
		all = mergeResult(all, backup)
		if backupErr != nil {
			if started {
				stop, _ := runTimeout(3*time.Minute, "gcloud", "compute", "instances", "stop", vmName, "--project="+cfg.Project, "--zone="+zone, "--quiet")
				all = mergeResult(all, stop)
			}
			return actionDoneMsg{"Rebuild", cfg, usefulOutput(all), fmt.Errorf("pre-rebuild backup failed: %w", backupErr)}
		}

		deleted, deleteErr := runTimeout(4*time.Minute, "gcloud", "compute", "instances", "delete", vmName,
			"--project="+cfg.Project, "--zone="+zone, "--delete-disks=all", "--quiet")
		all = mergeResult(all, deleted)
		if deleteErr != nil {
			return actionDoneMsg{"Rebuild", cfg, usefulOutput(all), deleteErr}
		}
		return rebuildReadyMsg{cfg, billingID}
	}
}

func destroyCmd(cfg config, releaseIP bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var all commandResult
		r, e := run(ctx, "gcloud", "compute", "instances", "delete", vmName, "--project="+cfg.Project, "--zone="+zone, "--delete-disks=all", "--quiet")
		all = mergeResult(all, r)
		if e != nil && !looksNotFound(all.Stderr) {
			return actionDoneMsg{"Destroy", cfg, usefulOutput(all), e}
		}
		if releaseIP {
			r, e = run(ctx, "gcloud", "compute", "addresses", "delete", addressName, "--project="+cfg.Project, "--region="+region, "--quiet")
			all = mergeResult(all, r)
			if e != nil && !looksNotFound(all.Stderr) {
				return actionDoneMsg{"Destroy", cfg, usefulOutput(all), e}
			}
		}
		cfg.setDisabled(cfg.Account, true)
		if e = saveConfig(cfg); e != nil {
			return actionDoneMsg{"Destroy", cfg, usefulOutput(all), e}
		}
		return actionDoneMsg{"Destroy", cfg, usefulOutput(all), nil}
	}
}

func runTimeout(timeout time.Duration, name string, args ...string) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return run(ctx, name, args...)
}

func run(ctx context.Context, name string, args ...string) (commandResult, error) {
	return runExec(exec.CommandContext(ctx, name, args...))
}

func runExec(cmd *exec.Cmd) (commandResult, error) {
	var out, errout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	r := commandResult{strings.TrimSpace(out.String()), strings.TrimSpace(errout.String()), shellDisplay(cmd.Args)}
	if err != nil && r.Stderr == "" {
		r.Stderr = err.Error()
	}
	return r, err
}
