package main

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const billingCreateURL = "https://console.cloud.google.com/billing/create"

func billingSetupURL(account string) string {
	q := url.Values{}
	q.Set("Email", account)
	q.Set("continue", billingCreateURL)
	return "https://accounts.google.com/AccountChooser?" + q.Encode()
}

func copyBillingLinkCmd(account string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("clip")
		cmd.Stdin = strings.NewReader(billingSetupURL(account))
		err := cmd.Run()
		return billingActionMsg{status: "Setup link copied", err: err}
	}
}

func billingQRCmd(account string) tea.Cmd {
	cmd := exec.Command(os.Args[0], "__billing-qr", account)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return billingActionMsg{refresh: err == nil, err: err}
	})
}

func runBillingQR(account string) error {
	setupURL := billingSetupURL(account)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Billing")
	fmt.Fprintln(os.Stdout, account)
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Scan this with the person who owns this Google account.")
	renderQR(os.Stdout, setupURL)
	fmt.Fprintln(os.Stdout, "Google Cloud will handle any first-time setup on that device.")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, setupURL)
	fmt.Fprintln(os.Stdout)
	fmt.Fprint(os.Stdout, "Press Enter after billing is created...")
	_, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err == io.EOF {
		return nil
	}
	return err
}
