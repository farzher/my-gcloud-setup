package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"time"
)

type externalAction int

const (
	externalNone externalAction = iota
	externalSSH
	externalHermes
	externalGateway
)

func runExternalSession(action externalAction, cfg config) error {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("OpenSSH client not found in PATH")
	}

	prepared, err := runTimeout(90*time.Second, "gcloud", "compute", "config-ssh", "--project="+cfg.Project, "--quiet")
	if err != nil {
		return fmt.Errorf("prepare SSH config: %w\n%s", err, usefulOutput(prepared))
	}

	host := vmName + "." + cfg.zone() + "." + cfg.Project
	var name string
	var args []string
	switch action {
	case externalSSH:
		name = "SSH"
		args = []string{"-tt", host}
	case externalHermes:
		name = "Hermes"
		remote := "sudo -n -i bash -lc " + shellQuote("cd /website/app 2>/dev/null || cd /root; exec hermes")
		args = []string{"-tt", host, remote}
	case externalGateway:
		name = "Gateway"
		args = []string{"-tt", host, "sudo -n -i hermes gateway setup"}
	default:
		return nil
	}

	cmd := exec.Command(ssh, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// Bubble Tea has just released the console. Discard pending menu key events
	// so they cannot become input to the remote program.
	flushConsoleInput()
	fmt.Print("\x1b[2J\x1b[H")
	fmt.Printf("cloud · %s\n\n", name)

	// On Windows Ctrl+C is delivered to every process attached to the console.
	// Catch it in cloud while the child owns the terminal; SSH/Hermes receives
	// its own console event and can handle it normally.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	err = cmd.Run()
	signal.Stop(interrupts)

	if action == externalGateway && err == nil {
		fmt.Println("\nStarting gateway service…")
		serviceRemote := "sudo -n -i bash -lc " + shellQuote("set -e; loginctl enable-linger root; hermes gateway install; hermes gateway start; hermes gateway status")
		service := exec.Command(ssh, host, serviceRemote)
		service.Stdin, service.Stdout, service.Stderr = os.Stdin, os.Stdout, os.Stderr
		err = service.Run()
	}

	flushConsoleInput()
	fmt.Print("\x1b[2J\x1b[H")

	// Ctrl+C is a normal way to leave Hermes/Gateway and return to cloud.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 130 {
		return nil
	}
	return err
}
