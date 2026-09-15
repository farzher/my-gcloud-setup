package main

import (
	"fmt"
	"os"
	"os/exec"
)

type externalAction int

const (
	externalNone externalAction = iota
	externalSSH
	externalHermes
	externalGateway
)

func runExternalSession(action externalAction, cfg config) error {
	var name string
	var cmd *exec.Cmd
	switch action {
	case externalSSH:
		name = "SSH"
		cmd = exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+cfg.zone())
	case externalHermes:
		name = "Hermes"
		remote := `exec sudo -n -i bash -lc 'cd /website/app 2>/dev/null || cd /root; exec hermes'`
		cmd = exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+cfg.zone(), "--command="+remote, "--", "-t")
	case externalGateway:
		name = "Gateway"
		remote := `exec sudo -n -i hermes gateway setup`
		cmd = exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+cfg.zone(), "--command="+remote, "--", "-t")
	default:
		return nil
	}

	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("cloud · %s\n\n", name)
	err := cmd.Run()
	fmt.Print("\x1b[2J\x1b[H")
	return err
}
