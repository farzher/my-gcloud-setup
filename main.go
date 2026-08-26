package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	qrterminal "github.com/mdp/qrterminal/v3"
)

const (
	appName = "cloud"

	region      = "us-east1"
	zone        = "us-east1-b"
	vmName      = "server"
	networkName = "cloud-net"
	subnetName  = "cloud-subnet"
	firewall    = "cloud-web"
	addressName = "cloud-ip"
	networkTag  = "cloud-web"
	adminEmail  = "stephenkamenar@gmail.com"

	billingURL = "https://console.cloud.google.com/billing/create"
	gcloudURL  = "https://cloud.google.com/sdk/docs/install"
)

const bootstrapScript = `#!/bin/bash
set -Eeuo pipefail
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

apt-get update
apt-get install -y --no-install-recommends ca-certificates curl git xz-utils

# The official installer is idempotent: an existing git install is updated.
curl -fsSL https://hermes-agent.nousresearch.com/install.sh \
  | bash -s -- --skip-setup --skip-browser --skip-computer-use

command -v hermes >/dev/null
hermes --version
# Doctor is useful diagnostics, but provider configuration may not exist yet.
hermes doctor || true
`

type screen int

const (
	screenLoading screen = iota
	screenNeedGcloud
	screenSignIn
	screenBilling
	screenBillingPick
	screenCreate
	screenProvision
	screenDashboard
	screenConfirm
	screenDetails
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmRebuild
	confirmDestroy
)

type config struct {
	Projects map[string]string `json:"projects,omitempty"`

	// Runtime-only fields. Each Google account gets one remembered managed project.
	Account string `json:"-"`
	Project string `json:"-"`
}

func (c config) projectFor(account string) string {
	if c.Projects == nil {
		return ""
	}
	return c.Projects[account]
}

func (c *config) setProject(account, project string) {
	if c.Projects == nil {
		c.Projects = map[string]string{}
	}
	c.Projects[account] = project
	c.Account = account
	c.Project = project
}

type billingAccount struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Open        bool   `json:"open"`
}

func (b billingAccount) id() string {
	return strings.TrimPrefix(b.Name, "billingAccounts/")
}

func billingListContains(accounts []billingAccount, id string) bool {
	for _, account := range accounts {
		if account.id() == id {
			return true
		}
	}
	return false
}

type instanceInfo struct {
	Status            string `json:"status"`
	MachineType       string `json:"machineType"`
	NetworkInterfaces []struct {
		AccessConfigs []struct {
			NatIP string `json:"natIP"`
		} `json:"accessConfigs"`
	} `json:"networkInterfaces"`
}

func (i instanceInfo) ip() string {
	if len(i.NetworkInterfaces) == 0 || len(i.NetworkInterfaces[0].AccessConfigs) == 0 {
		return ""
	}
	return i.NetworkInterfaces[0].AccessConfigs[0].NatIP
}

func (i instanceInfo) machine() string {
	parts := strings.Split(i.MachineType, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

type cloudState struct {
	Gcloud      bool
	Account     string
	Billing     []billingAccount
	ProjectOK   bool
	VMExists    bool
	Instance    instanceInfo
	StaticIP    string
	HermesReady bool
}

type provisionStep struct {
	Name   string
	State  int // 0 pending, 1 running, 2 done, 3 failed
	Detail string
}

type model struct {
	screen screen
	width  int
	height int
	frame  int

	cfg        config
	state      cloudState
	billing    []billingAccount
	billingPos int
	billingID  string
	signInPos  int

	menu    []string
	menuPos int

	steps       []provisionStep
	stepIndex   int
	busy        bool
	lastErr     error
	lastOutput  string
	lastCommand string

	confirm    confirmKind
	confirmPos int

	returnScreen screen
	statusText   string
}

type tickMsg time.Time

type detectedMsg struct {
	state cloudState
	err   error
}

type authFinishedMsg struct{ err error }
type externalFinishedMsg struct{ err error }
type actionFinishedMsg struct {
	name   string
	output string
	err    error
}
type provisionFinishedMsg struct {
	index   int
	cfg     config
	detail  string
	output  string
	command string
	err     error
}

type browserOpenedMsg struct{ err error }
type piFinishedMsg struct{ err error }

type commandResult struct {
	Stdout  string
	Stderr  string
	Command string
}

var (
	accent = lipgloss.Color("#C084FC")
	green  = lipgloss.Color("#86EFAC")
	yellow = lipgloss.Color("#FDE68A")
	red    = lipgloss.Color("#FCA5A5")
	muted  = lipgloss.Color("#7C8394")
	bright = lipgloss.Color("#F4F4F5")

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(bright)
	accentStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(muted)
	goodStyle   = lipgloss.NewStyle().Foreground(green)
	warnStyle   = lipgloss.NewStyle().Foreground(yellow)
	badStyle    = lipgloss.NewStyle().Foreground(red)
)

func initialModel() model {
	return model{
		screen: screenLoading,
		cfg:    loadConfig(),
		menu:   []string{"Hermes", "SSH", "Restart", "Stop", "Rebuild", "Destroy", "Account"},
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(detectCmd(m.cfg), tick())
}

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.frame++
		if m.screen == screenBilling && !m.busy && m.frame%40 == 0 {
			m.busy = true
			return m, tea.Batch(tick(), detectCmd(m.cfg))
		}
		return m, tick()

	case detectedMsg:
		m.busy = false
		m.statusText = ""
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
			m.returnScreen = screenLoading
			m.screen = screenDetails
			return m, nil
		}
		previousAccount := m.state.Account
		if previousAccount != msg.state.Account {
			m.billingID = ""
			m.billingPos = 0
		}
		if m.billingID != "" && !billingListContains(msg.state.Billing, m.billingID) {
			m.billingID = ""
			m.billingPos = 0
		}
		m.state = msg.state
		m.billing = msg.state.Billing
		m.cfg.Account = msg.state.Account
		m.cfg.Project = m.cfg.projectFor(msg.state.Account)
		m.lastErr = nil
		m.routeAfterDetect()
		return m, nil

	case authFinishedMsg:
		m.busy = true
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
		}
		return m, detectCmd(m.cfg)

	case rebuildPreparedMsg:
		m.confirm = confirmNone
		m.statusText = ""
		m.startProvision()
		for i := 0; i < 5; i++ {
			m.steps[i].State = 2
		}
		m.stepIndex = 5
		m.steps[m.stepIndex].State = 1
		m.busy = true
		return m, runProvisionStepCmd(m.stepIndex, msg.cfg, msg.billingID)

	case piFinishedMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
			m.returnScreen = screenProvision
			m.screen = screenDetails
			return m, nil
		}
		if m.stepIndex >= 0 && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
			m.screen = screenProvision
			m.steps[m.stepIndex].State = 1
			m.busy = true
			return m, runProvisionStepCmd(m.stepIndex, m.cfg, m.billingID)
		}
		m.busy = true
		return m, detectCmd(m.cfg)

	case browserOpenedMsg:
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
			m.returnScreen = m.screen
			m.screen = screenDetails
		}
		return m, nil

	case externalFinishedMsg:
		if msg.err != nil {
			m.busy = false
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
			m.returnScreen = screenDashboard
			m.screen = screenDetails
			return m, nil
		}
		m.busy = true
		return m, detectCmd(m.cfg)

	case actionFinishedMsg:
		m.busy = false
		m.statusText = ""
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.output
			m.returnScreen = screenDashboard
			m.screen = screenDetails
			return m, nil
		}
		m.statusText = msg.name + " complete"
		m.busy = true
		return m, detectCmd(m.cfg)

	case provisionFinishedMsg:
		m.busy = false
		if msg.index < 0 || msg.index >= len(m.steps) {
			return m, nil
		}
		m.cfg = msg.cfg
		m.lastOutput = msg.output
		m.lastCommand = msg.command
		if msg.err != nil {
			m.steps[msg.index].State = 3
			m.steps[msg.index].Detail = shortError(msg.err)
			m.lastErr = msg.err
			return m, nil
		}
		m.steps[msg.index].State = 2
		m.steps[msg.index].Detail = msg.detail
		m.stepIndex = msg.index + 1
		if m.stepIndex >= len(m.steps) {
			m.busy = true
			return m, detectCmd(m.cfg)
		}
		m.steps[m.stepIndex].State = 1
		m.busy = true
		return m, runProvisionStepCmd(m.stepIndex, m.cfg, m.billingID)
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	k := key.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.screen {
	case screenLoading:
		if k == "q" {
			return m, tea.Quit
		}

	case screenNeedGcloud:
		switch k {
		case "enter", "o":
			return m, openBrowserCmd(gcloudURL)
		case "r":
			m.busy = true
			return m, detectCmd(m.cfg)
		case "q":
			return m, tea.Quit
		}

	case screenSignIn:
		switch k {
		case "up", "k", "left", "h":
			if m.signInPos > 0 {
				m.signInPos--
			}
		case "down", "j", "right", "l", "tab":
			if m.signInPos < 1 {
				m.signInPos++
			} else if k == "tab" {
				m.signInPos = 0
			}
		case "1", "b":
			m.busy = true
			return m, authCmd()
		case "2":
			m.busy = true
			return m, remoteAuthCmd()
		case "enter":
			m.busy = true
			if m.signInPos == 1 {
				return m, remoteAuthCmd()
			}
			return m, authCmd()
		case "r":
			m.busy = true
			return m, detectCmd(m.cfg)
		case "q":
			return m, tea.Quit
		}

	case screenBilling:
		switch k {
		case "enter", "o":
			return m, openBrowserCmd(billingURL)
		case "r":
			m.busy = true
			return m, detectCmd(m.cfg)
		case "q":
			return m, tea.Quit
		}

	case screenBillingPick:
		switch k {
		case "up", "k":
			if m.billingPos > 0 {
				m.billingPos--
			}
		case "down", "j":
			if m.billingPos < len(m.billing)-1 {
				m.billingPos++
			}
		case "enter":
			if len(m.billing) > 0 {
				m.billingID = m.billing[m.billingPos].id()
				m.screen = screenCreate
			}
		case "q":
			return m, tea.Quit
		}

	case screenCreate:
		switch k {
		case "enter":
			if m.billingID == "" && len(m.billing) == 1 {
				m.billingID = m.billing[0].id()
			}
			if m.billingID == "" {
				m.screen = screenBillingPick
				return m, nil
			}
			m.startProvision()
			return m, runProvisionStepCmd(0, m.cfg, m.billingID)
		case "a":
			m.busy = true
			return m, authCmd()
		case "q":
			return m, tea.Quit
		}

	case screenProvision:
		if k == "q" && !m.busy {
			return m, tea.Quit
		}
		if !m.busy && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
			switch k {
			case "r", "enter":
				m.steps[m.stepIndex].State = 1
				m.busy = true
				return m, runProvisionStepCmd(m.stepIndex, m.cfg, m.billingID)
			case "d":
				m.returnScreen = screenProvision
				m.screen = screenDetails
			case "p":
				if hasExecutable("pi") {
					return m, piCmd(m)
				}
			}
		}

	case screenDashboard:
		if m.busy {
			return m, nil
		}
		switch k {
		case "up", "k":
			if m.menuPos > 0 {
				m.menuPos--
			}
		case "down", "j":
			if m.menuPos < len(m.menu)-1 {
				m.menuPos++
			}
		case "r":
			m.busy = true
			m.statusText = "Refreshing"
			return m, detectCmd(m.cfg)
		case "q":
			return m, tea.Quit
		case "enter":
			return m.activateMenu()
		}

	case screenConfirm:
		max := 1
		if m.confirm == confirmDestroy {
			max = 2
		}
		switch k {
		case "up", "k":
			if m.confirmPos > 0 {
				m.confirmPos--
			}
		case "down", "j":
			if m.confirmPos < max {
				m.confirmPos++
			}
		case "esc", "q":
			m.screen = screenDashboard
			m.confirm = confirmNone
		case "enter":
			return m.runConfirmedAction()
		}

	case screenDetails:
		switch k {
		case "esc", "q", "enter":
			m.screen = m.returnScreen
		case "p":
			if hasExecutable("pi") && m.lastErr != nil {
				return m, piCmd(m)
			}
		}
	}

	return m, nil
}

func (m *model) routeAfterDetect() {
	if !m.state.Gcloud {
		m.screen = screenNeedGcloud
		return
	}
	if m.state.Account == "" {
		m.screen = screenSignIn
		return
	}
	if m.cfg.Project != "" && m.state.VMExists {
		m.screen = screenDashboard
		m.syncMenu()
		return
	}
	if len(m.state.Billing) == 0 {
		m.screen = screenBilling
		return
	}
	if len(m.state.Billing) == 1 {
		m.billingID = m.state.Billing[0].id()
		m.screen = screenCreate
		return
	}
	if m.billingID == "" {
		m.screen = screenBillingPick
		return
	}
	m.screen = screenCreate
}

func (m *model) syncMenu() {
	power := "Stop"
	if strings.EqualFold(m.state.Instance.Status, "TERMINATED") || strings.EqualFold(m.state.Instance.Status, "STOPPED") {
		power = "Start"
	}
	m.menu = []string{"Hermes", "SSH", "Restart", power, "Rebuild", "Destroy", "Account"}
	if m.menuPos >= len(m.menu) {
		m.menuPos = len(m.menu) - 1
	}
}

func (m *model) startProvision() {
	m.screen = screenProvision
	m.busy = true
	m.lastErr = nil
	m.lastOutput = ""
	m.lastCommand = ""
	m.stepIndex = 0
	m.steps = []provisionStep{
		{Name: "Project", State: 1},
		{Name: "Billing"},
		{Name: "Compute Engine"},
		{Name: "Network"},
		{Name: "Static IP"},
		{Name: "Debian VM"},
		{Name: "SSH"},
		{Name: "Hermes"},
		{Name: "Verify"},
	}
}

func (m model) activateMenu() (tea.Model, tea.Cmd) {
	if m.menuPos < 0 || m.menuPos >= len(m.menu) {
		return m, nil
	}
	action := m.menu[m.menuPos]
	switch action {
	case "Hermes":
		return m, remoteHermesCmd(m.cfg)
	case "SSH":
		return m, remoteSSHCmd(m.cfg)
	case "Restart":
		m.busy = true
		m.statusText = "Restarting"
		return m, lifecycleCmd("Restart", m.cfg, "reset")
	case "Stop":
		m.busy = true
		m.statusText = "Stopping"
		return m, lifecycleCmd("Stop", m.cfg, "stop")
	case "Start":
		m.busy = true
		m.statusText = "Starting"
		return m, lifecycleCmd("Start", m.cfg, "start")
	case "Rebuild":
		m.screen = screenConfirm
		m.confirm = confirmRebuild
		m.confirmPos = 1 // default Cancel
		return m, nil
	case "Destroy":
		m.screen = screenConfirm
		m.confirm = confirmDestroy
		m.confirmPos = 2 // default Cancel
		return m, nil
	case "Account":
		m.busy = true
		return m, authCmd()
	}
	return m, nil
}

func (m model) runConfirmedAction() (tea.Model, tea.Cmd) {
	switch m.confirm {
	case confirmRebuild:
		if m.confirmPos == 0 {
			m.screen = screenProvision
			m.busy = true
			m.statusText = "Deleting old VM"
			return m, rebuildDeleteCmd(m.cfg, m.billingID)
		}
		m.screen = screenDashboard
		m.confirm = confirmNone

	case confirmDestroy:
		switch m.confirmPos {
		case 0:
			m.screen = screenDashboard
			m.busy = true
			m.statusText = "Destroying VM"
			return m, destroyCmd(m.cfg, false)
		case 1:
			m.screen = screenDashboard
			m.busy = true
			m.statusText = "Destroying VM and IP"
			return m, destroyCmd(m.cfg, true)
		default:
			m.screen = screenDashboard
			m.confirm = confirmNone
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	content := m.render()
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}

	boxWidth := 50
	if w < boxWidth+4 {
		boxWidth = max(34, w-4)
	}
	card := lipgloss.NewStyle().
		Width(boxWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Render(content)

	view := tea.NewView(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, card))
	view.AltScreen = true
	return view
}

func (m model) render() string {
	switch m.screen {
	case screenLoading:
		return m.header() + "\n\n" + spinner(m.frame) + " gcloud"
	case screenNeedGcloud:
		return m.header() + "\n\n" +
			badStyle.Render("gcloud not found") + "\n\n" +
			button("Install gcloud", true) + "\n\n" +
			mutedStyle.Render("enter  r  q")
	case screenSignIn:
		return m.renderSignIn()
	case screenBilling:
		return m.header() + "\n\n" +
			goodStyle.Render("✓ "+m.state.Account) + "\n\n" +
			titleStyle.Render("Billing") + "\n\n" +
			button("Open billing", true) + "\n\n" +
			mutedStyle.Render("enter  r  q")
	case screenBillingPick:
		return m.renderBillingPick()
	case screenCreate:
		return m.renderCreate()
	case screenProvision:
		return m.renderProvision()
	case screenDashboard:
		return m.renderDashboard()
	case screenConfirm:
		return m.renderConfirm()
	case screenDetails:
		return m.renderDetails()
	default:
		return m.header()
	}
}

func (m model) renderSignIn() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("Sign in"))
	b.WriteString("\n\n")
	b.WriteString(loginChoice("Browser", m.signInPos == 0))
	b.WriteString("\n")
	b.WriteString(loginChoice("QR code", m.signInPos == 1))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("↑/↓  enter  q"))
	return b.String()
}

func loginChoice(title string, active bool) string {
	marker := "  "
	border := muted
	if active {
		marker, border = "› ", accent
	}
	style := lipgloss.NewStyle().
		Width(24).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border)
	if active {
		style = style.Foreground(bright).Bold(true)
	}
	return style.Render(marker + title)
}

func (m model) header() string {
	return accentStyle.Render("●") + " " + titleStyle.Render(appName)
}

func (m model) renderBillingPick() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("Billing"))
	b.WriteString("\n\n")
	for i, a := range m.billing {
		name := a.DisplayName
		if name == "" {
			name = a.id()
		}
		if i == m.billingPos {
			b.WriteString(accentStyle.Render("› " + name))
		} else {
			b.WriteString("  " + name)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render("↑/↓  enter  q"))
	return b.String()
}

func (m model) renderCreate() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	if m.cfg.Project == "" {
		b.WriteString(titleStyle.Render("New server"))
	} else {
		b.WriteString(titleStyle.Render("Resume"))
	}
	b.WriteString("\n\n")
	b.WriteString("e2-micro · Debian 13\n")
	b.WriteString("30 GB pd-standard\n")
	b.WriteString(zone + " · Premium · static IPv4\n")
	b.WriteString("22 · 80 · 443 · Hermes root\n\n")
	b.WriteString(button("Create", true))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("enter  a account  q"))
	return b.String()
}

func (m model) renderProvision() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("Provision"))
	b.WriteString("\n\n")
	for _, s := range m.steps {
		icon := mutedStyle.Render("·")
		switch s.State {
		case 1:
			icon = accentStyle.Render(spinner(m.frame))
		case 2:
			icon = goodStyle.Render("✓")
		case 3:
			icon = badStyle.Render("✕")
		}
		line := fmt.Sprintf("%s %-18s", icon, s.Name)
		if s.Detail != "" {
			line += " " + mutedStyle.Render(s.Detail)
		}
		b.WriteString(line + "\n")
	}
	if !m.busy && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
		b.WriteString("\n")
		b.WriteString(badStyle.Render(shortError(m.lastErr)))
		b.WriteString("\n\n")
		b.WriteString(mutedStyle.Render("r retry  d details"))
		if hasExecutable("pi") {
			b.WriteString(mutedStyle.Render("  p Pi"))
		}
	}
	return b.String()
}

func (m model) renderDashboard() string {
	status := strings.ToUpper(m.state.Instance.Status)
	statusView := goodStyle.Render("● " + status)
	if status != "RUNNING" {
		statusView = warnStyle.Render("● " + status)
	}
	ip := m.state.Instance.ip()
	if ip == "" {
		ip = m.state.StaticIP
	}
	if ip == "" {
		ip = "—"
	}

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render(vmName) + "  " + statusView + "\n")
	b.WriteString(titleStyle.Render(ip) + "\n")
	b.WriteString(mutedStyle.Render(m.state.Account) + "\n\n")
	for i, item := range m.menu {
		if i == m.menuPos {
			b.WriteString(accentStyle.Render("› " + item))
		} else {
			b.WriteString("  " + item)
		}
		b.WriteString("\n")
	}
	if m.statusText != "" {
		b.WriteString("\n" + spinner(m.frame) + " " + m.statusText)
	}
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("↑/↓  enter  r  q"))
	return b.String()
}

func (m model) renderConfirm() string {
	var title string
	var opts []string
	switch m.confirm {
	case confirmRebuild:
		title = "Rebuild?"
		opts = []string{"Rebuild · keep IP", "Cancel"}
	case confirmDestroy:
		title = "Destroy?"
		opts = []string{"VM · keep IP", "VM + IP", "Cancel"}
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n\n")
	b.WriteString(badStyle.Render(title) + "\n\n")
	for i, opt := range opts {
		if i == m.confirmPos {
			b.WriteString(accentStyle.Render("› " + opt))
		} else {
			b.WriteString("  " + opt)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + mutedStyle.Render("↑/↓  enter  esc"))
	return b.String()
}

func (m model) renderDetails() string {
	text := strings.TrimSpace(m.lastOutput)
	if text == "" && m.lastErr != nil {
		text = m.lastErr.Error()
	}
	if text == "" {
		text = "—"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 14 {
		lines = lines[len(lines)-14:]
	}
	for i := range lines {
		if len(lines[i]) > 88 {
			lines[i] = lines[i][:88] + "…"
		}
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n\n")
	if m.lastErr != nil {
		b.WriteString(badStyle.Render("Error") + "\n\n")
	} else {
		b.WriteString(titleStyle.Render("Details") + "\n\n")
	}
	if m.lastCommand != "" {
		b.WriteString(mutedStyle.Render("$ "+m.lastCommand) + "\n\n")
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n\n")
	b.WriteString(mutedStyle.Render("enter/esc"))
	if hasExecutable("pi") && m.lastErr != nil {
		b.WriteString(mutedStyle.Render("  p Pi"))
	}
	return b.String()
}

func button(label string, active bool) string {
	s := lipgloss.NewStyle().Padding(0, 2).Border(lipgloss.RoundedBorder())
	if active {
		s = s.Foreground(bright).BorderForeground(accent).Bold(true)
	} else {
		s = s.Foreground(muted).BorderForeground(muted)
	}
	return s.Render(label)
}

func spinner(frame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return accentStyle.Render(frames[frame%len(frames)])
}

func detectCmd(cfg config) tea.Cmd {
	return func() tea.Msg {
		state, err := detect(cfg)
		return detectedMsg{state: state, err: err}
	}
}

func detect(cfg config) (cloudState, error) {
	var s cloudState
	if !hasExecutable("gcloud") {
		return s, nil
	}
	s.Gcloud = true

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := run(ctx, "gcloud", "auth", "list", "--filter=status:ACTIVE", "--format=value(account)")
	if err != nil {
		return s, fmt.Errorf("gcloud auth check failed: %w\n%s", err, usefulOutput(res))
	}
	s.Account = firstLine(res.Stdout)
	if s.Account == "" {
		return s, nil
	}

	res, err = run(ctx, "gcloud", "billing", "accounts", "list", "--filter=open=true", "--format=json")
	if err == nil && strings.TrimSpace(res.Stdout) != "" {
		_ = json.Unmarshal([]byte(res.Stdout), &s.Billing)
	}

	project := cfg.projectFor(s.Account)
	if project == "" {
		return s, nil
	}

	res, err = run(ctx, "gcloud", "projects", "describe", project, "--format=value(projectId)")
	if err != nil {
		// The local config can outlive a project. Treat a missing/inaccessible
		// project as not present instead of trapping the user in an error screen.
		return s, nil
	}
	s.ProjectOK = strings.TrimSpace(res.Stdout) != ""

	owner, err := projectOwner(ctx, project, adminEmail)
	if err != nil {
		return s, fmt.Errorf("project owner check failed: %w", err)
	}
	if !owner {
		if _, err := ensureProjectOwner(project); err != nil {
			return s, fmt.Errorf("project owner setup failed: %w", err)
		}
	}

	res, err = run(ctx, "gcloud", "compute", "instances", "describe", vmName,
		"--project="+project, "--zone="+zone, "--format=json")
	if err == nil && strings.TrimSpace(res.Stdout) != "" {
		s.VMExists = true
		_ = json.Unmarshal([]byte(res.Stdout), &s.Instance)
	}

	res, err = run(ctx, "gcloud", "compute", "addresses", "describe", addressName,
		"--project="+project, "--region="+region, "--format=value(address)")
	if err == nil {
		s.StaticIP = firstLine(res.Stdout)
	}
	return s, nil
}

func authCmd() tea.Cmd {
	cmd := exec.Command("gcloud", "auth", "login")
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authFinishedMsg{err: err} })
}

func remoteAuthCmd() tea.Cmd {
	cmd := exec.Command(os.Args[0], "__remote-auth")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return authFinishedMsg{err: err} })
}

func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		return browserOpenedMsg{err: openBrowser(url)}
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func runProvisionStepCmd(index int, cfg config, billingID string) tea.Cmd {
	return func() tea.Msg {
		newCfg, detail, res, err := provisionStepRun(index, cfg, billingID)
		return provisionFinishedMsg{
			index:   index,
			cfg:     newCfg,
			detail:  detail,
			output:  usefulOutput(res),
			command: res.Command,
			err:     err,
		}
	}
}

func provisionStepRun(index int, cfg config, billingID string) (config, string, commandResult, error) {
	var zero commandResult
	switch index {
	case 0:
		return ensureProject(cfg)
	case 1:
		if cfg.Project == "" {
			return cfg, "", zero, errors.New("project is not set")
		}
		if billingID == "" {
			return cfg, "", zero, errors.New("billing account is not selected")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		res, err := run(ctx, "gcloud", "billing", "projects", "link", cfg.Project,
			"--billing-account="+billingID, "--quiet")
		return cfg, billingID, res, err
	case 2:
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		res, err := run(ctx, "gcloud", "services", "enable", "compute.googleapis.com",
			"--project="+cfg.Project, "--quiet")
		return cfg, "enabled", res, err
	case 3:
		res, err := ensureNetwork(cfg)
		return cfg, "22 · 80 · 443", res, err
	case 4:
		res, ip, err := ensureAddress(cfg)
		return cfg, ip, res, err
	case 5:
		res, err := ensureVM(cfg)
		return cfg, "e2-micro", res, err
	case 6:
		res, err := waitForSSH(cfg)
		return cfg, "ready", res, err
	case 7:
		res, err := installHermes(cfg)
		return cfg, "root", res, err
	case 8:
		res, detail, err := verifyRemote(cfg)
		return cfg, detail, res, err
	default:
		return cfg, "", zero, fmt.Errorf("unknown provision step %d", index)
	}
}

func ensureProject(cfg config) (config, string, commandResult, error) {
	if cfg.Project != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := run(ctx, "gcloud", "projects", "describe", cfg.Project, "--format=value(projectId)")
		if err == nil {
			ownerRes, ownerErr := ensureProjectOwner(cfg.Project)
			return cfg, cfg.Project, mergeResult(res, ownerRes), ownerErr
		}
		// A stale config should not cause us to create resources in a project
		// the user can no longer access. Clear it and create a new app project.
		if cfg.Projects != nil && cfg.Account != "" {
			delete(cfg.Projects, cfg.Account)
		}
		cfg.Project = ""
	}

	var last commandResult
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		id := "cloud-" + randomHex(5)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		res, err := run(ctx, "gcloud", "projects", "create", id,
			"--name=Cloud server", "--labels=cloud-charm=managed", "--quiet")
		cancel()
		last, lastErr = res, err
		if err == nil {
			if cfg.Account == "" {
				return cfg, "", res, errors.New("active Google account is unknown")
			}
			cfg.setProject(cfg.Account, id)
			if saveErr := saveConfig(cfg); saveErr != nil {
				return cfg, "", res, saveErr
			}
			ownerRes, ownerErr := ensureProjectOwner(id)
			res = mergeResult(res, ownerRes)
			if ownerErr != nil {
				return cfg, "", res, fmt.Errorf("grant %s owner: %w", adminEmail, ownerErr)
			}
			return cfg, id, res, nil
		}
	}
	return cfg, "", last, fmt.Errorf("could not create a unique project after retries: %w", lastErr)
}

func projectOwner(ctx context.Context, project, email string) (bool, error) {
	res, err := run(ctx, "gcloud", "projects", "get-iam-policy", project,
		"--flatten=bindings[].members",
		"--filter=bindings.role:roles/owner AND bindings.members:user:"+email,
		"--format=value(bindings.members)")
	if err != nil {
		return false, err
	}
	return strings.Contains(res.Stdout, "user:"+email), nil
}

func ensureProjectOwner(project string) (commandResult, error) {
	var last commandResult
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		last, err = run(ctx, "gcloud", "projects", "add-iam-policy-binding", project,
			"--member=user:"+adminEmail,
			"--role=roles/owner",
			"--condition=None",
			"--quiet")
		cancel()
		if err == nil {
			return last, nil
		}
		time.Sleep(2 * time.Second)
	}
	return last, err
}

func ensureNetwork(cfg config) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var combined commandResult

	if !gcloudExists(ctx, "compute", "networks", "describe", networkName, "--project="+cfg.Project) {
		res, err := run(ctx, "gcloud", "compute", "networks", "create", networkName,
			"--project="+cfg.Project, "--subnet-mode=custom", "--quiet")
		combined = mergeResult(combined, res)
		if err != nil {
			return combined, err
		}
	}

	if !gcloudExists(ctx, "compute", "networks", "subnets", "describe", subnetName,
		"--project="+cfg.Project, "--region="+region) {
		res, err := run(ctx, "gcloud", "compute", "networks", "subnets", "create", subnetName,
			"--project="+cfg.Project, "--region="+region, "--network="+networkName,
			"--range=10.10.0.0/24", "--quiet")
		combined = mergeResult(combined, res)
		if err != nil {
			return combined, err
		}
	}

	if !gcloudExists(ctx, "compute", "firewall-rules", "describe", firewall, "--project="+cfg.Project) {
		res, err := run(ctx, "gcloud", "compute", "firewall-rules", "create", firewall,
			"--project="+cfg.Project, "--network="+networkName,
			"--direction=INGRESS", "--allow=tcp:22,tcp:80,tcp:443",
			"--source-ranges=0.0.0.0/0", "--target-tags="+networkTag, "--quiet")
		combined = mergeResult(combined, res)
		if err != nil {
			return combined, err
		}
	}
	return combined, nil
}

func ensureAddress(cfg config) (commandResult, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var combined commandResult
	if !gcloudExists(ctx, "compute", "addresses", "describe", addressName,
		"--project="+cfg.Project, "--region="+region) {
		res, err := run(ctx, "gcloud", "compute", "addresses", "create", addressName,
			"--project="+cfg.Project, "--region="+region, "--network-tier=PREMIUM", "--quiet")
		combined = mergeResult(combined, res)
		if err != nil {
			return combined, "", err
		}
	}
	res, err := run(ctx, "gcloud", "compute", "addresses", "describe", addressName,
		"--project="+cfg.Project, "--region="+region, "--format=value(address)")
	combined = mergeResult(combined, res)
	if err != nil {
		return combined, "", err
	}
	ip := firstLine(res.Stdout)
	if ip == "" {
		return combined, "", errors.New("static IP exists but has no address")
	}
	return combined, ip, nil
}

func ensureVM(cfg config) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if gcloudExists(ctx, "compute", "instances", "describe", vmName,
		"--project="+cfg.Project, "--zone="+zone) {
		res, err := run(ctx, "gcloud", "compute", "instances", "describe", vmName,
			"--project="+cfg.Project, "--zone="+zone, "--format=value(status)")
		return res, err
	}
	res, err := run(ctx, "gcloud", "compute", "instances", "create", vmName,
		"--project="+cfg.Project,
		"--zone="+zone,
		"--machine-type=e2-micro",
		"--image-family=debian-13",
		"--image-project=debian-cloud",
		"--boot-disk-size=30GB",
		"--boot-disk-type=pd-standard",
		"--subnet="+subnetName,
		"--address="+addressName,
		"--network-tier=PREMIUM",
		"--tags="+networkTag,
		"--no-service-account",
		"--no-scopes",
		"--no-deletion-protection",
		"--quiet")
	return res, err
}

func waitForSSH(cfg config) (commandResult, error) {
	var last commandResult
	var err error
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		last, err = run(ctx, "gcloud", "compute", "ssh", vmName,
			"--project="+cfg.Project, "--zone="+zone,
			"--command=echo CLOUD_CHARM_SSH_OK", "--quiet")
		cancel()
		if err == nil && strings.Contains(last.Stdout, "CLOUD_CHARM_SSH_OK") {
			return last, nil
		}
		time.Sleep(3 * time.Second)
	}
	if err == nil {
		err = errors.New("SSH did not become ready")
	}
	return last, err
}

func installHermes(cfg config) (commandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone,
		"--command=sudo -n bash -s", "--quiet")
	cmd.Stdin = strings.NewReader(bootstrapScript)
	return runExec(cmd)
}

func verifyRemote(cfg config) (commandResult, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	remote := `set -e; . /etc/os-release; printf '%s %s\n' "$ID" "$VERSION_ID"; sudo -n /usr/local/bin/hermes --version`
	res, err := run(ctx, "gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone,
		"--command="+remote, "--quiet")
	if err != nil {
		return res, "", err
	}
	lines := nonEmptyLines(res.Stdout)
	if len(lines) == 0 || !strings.HasPrefix(strings.ToLower(lines[0]), "debian 13") {
		return res, "", fmt.Errorf("unexpected remote OS: %q", firstLine(res.Stdout))
	}
	if len(lines) < 2 {
		return res, "", errors.New("Hermes version check returned no output")
	}
	return res, lines[1], nil
}

func lifecycleCmd(name string, cfg config, action string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		res, err := run(ctx, "gcloud", "compute", "instances", action, vmName,
			"--project="+cfg.Project, "--zone="+zone, "--quiet")
		return actionFinishedMsg{name: name, output: usefulOutput(res), err: err}
	}
}

func rebuildDeleteCmd(cfg config, billingID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		res, err := run(ctx, "gcloud", "compute", "instances", "delete", vmName,
			"--project="+cfg.Project, "--zone="+zone, "--delete-disks=all", "--quiet")
		cancel()
		if err != nil {
			return actionFinishedMsg{name: "Rebuild", output: usefulOutput(res), err: err}
		}
		// Re-enter the normal idempotent provisioning pipeline from the VM step.
		return rebuildPreparedMsg{cfg: cfg, billingID: billingID}
	}
}

type rebuildPreparedMsg struct {
	cfg       config
	billingID string
}

func destroyCmd(cfg config, releaseIP bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var combined commandResult
		res, err := run(ctx, "gcloud", "compute", "instances", "delete", vmName,
			"--project="+cfg.Project, "--zone="+zone, "--delete-disks=all", "--quiet")
		combined = mergeResult(combined, res)
		if err != nil && !looksNotFound(combined.Stderr) {
			return actionFinishedMsg{name: "Destroy", output: usefulOutput(combined), err: err}
		}
		if releaseIP {
			res, err = run(ctx, "gcloud", "compute", "addresses", "delete", addressName,
				"--project="+cfg.Project, "--region="+region, "--quiet")
			combined = mergeResult(combined, res)
			if err != nil && !looksNotFound(combined.Stderr) {
				return actionFinishedMsg{name: "Destroy", output: usefulOutput(combined), err: err}
			}
		}
		return actionFinishedMsg{name: "Destroy", output: usefulOutput(combined), err: nil}
	}
}

func remoteSSHCmd(cfg config) tea.Cmd {
	cmd := exec.Command("gcloud", "compute", "ssh", vmName, "--project="+cfg.Project, "--zone="+zone)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return externalFinishedMsg{err: err} })
}

func remoteHermesCmd(cfg config) tea.Cmd {
	remote := `exec sudo -n -i bash -lc 'if hermes config get model.provider >/dev/null 2>&1; then exec hermes; else exec hermes setup; fi'`
	cmd := exec.Command("gcloud", "compute", "ssh", vmName,
		"--project="+cfg.Project, "--zone="+zone,
		"--command="+remote, "--", "-t")
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return externalFinishedMsg{err: err} })
}

func piCmd(m model) tea.Cmd {
	prompt := fmt.Sprintf(`Cloud server setup needs troubleshooting.

Goal: get the standard managed server into a healthy state. Use gcloud and SSH as needed, but do not redesign the infrastructure or create unrelated resources.

Project: %s
Zone: %s
VM: %s
IP: %s
SSH: gcloud compute ssh %s --project=%s --zone=%s
Failed step: %s
Command: %s
Error: %v

Recent output:
%s

Expected server: Debian 13, e2-micro, 30 GB pd-standard, Premium reserved IPv4, ports 22/80/443, Hermes installed system-wide as root. Diagnose the failure, fix only what is necessary, and verify the result.`,
		m.cfg.Project, zone, vmName, m.state.StaticIP, vmName, m.cfg.Project, zone,
		failedStepName(m), m.lastCommand, m.lastErr, m.lastOutput)
	cmd := exec.Command("pi", prompt)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return piFinishedMsg{err: err} })
}

func failedStepName(m model) string {
	if m.stepIndex >= 0 && m.stepIndex < len(m.steps) {
		return m.steps[m.stepIndex].Name
	}
	return "unknown"
}

func run(ctx context.Context, name string, args ...string) (commandResult, error) {
	return runExec(exec.CommandContext(ctx, name, args...))
}

func runExec(cmd *exec.Cmd) (commandResult, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := commandResult{
		Stdout:  strings.TrimSpace(stdout.String()),
		Stderr:  strings.TrimSpace(stderr.String()),
		Command: shellDisplay(cmd.Args),
	}
	if err != nil {
		if res.Stderr == "" {
			res.Stderr = err.Error()
		}
		return res, err
	}
	return res, nil
}

func gcloudExists(ctx context.Context, args ...string) bool {
	args = append(args, "--format=value(name)")
	_, err := run(ctx, "gcloud", args...)
	return err == nil
}

func mergeResult(a, b commandResult) commandResult {
	if a.Stdout != "" && b.Stdout != "" {
		a.Stdout += "\n"
	}
	a.Stdout += b.Stdout
	if a.Stderr != "" && b.Stderr != "" {
		a.Stderr += "\n"
	}
	a.Stderr += b.Stderr
	if b.Command != "" {
		a.Command = b.Command
	}
	return a
}

func usefulOutput(res commandResult) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(res.Stdout) != "" {
		parts = append(parts, strings.TrimSpace(res.Stdout))
	}
	if strings.TrimSpace(res.Stderr) != "" {
		parts = append(parts, strings.TrimSpace(res.Stderr))
	}
	return strings.Join(parts, "\n")
}

func shellDisplay(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\n\"'") {
			out[i] = fmt.Sprintf("%q", a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

func hasExecutable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func firstLine(s string) string {
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func looksNotFound(s string) bool {
	x := strings.ToLower(s)
	return strings.Contains(x, "not found") || strings.Contains(x, "was not found")
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 64 {
		s = s[:61] + "..."
	}
	return s
}

func randomHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n]
	}
	return hex.EncodeToString(b)[:n]
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "cloud-charm", "config.json")
}

func loadConfig() config {
	cfg := config{Projects: map[string]string{}}
	data, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	if cfg.Projects == nil {
		cfg.Projects = map[string]string{}
	}
	return cfg
}

func saveConfig(cfg config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

var authURLPattern = regexp.MustCompile(`https://accounts\.google\.com/[^\s]+`)

type authWriter struct {
	out   io.Writer
	buf   string
	shown bool
}

func (w *authWriter) Write(p []byte) (int, error) {
	if _, err := w.out.Write(p); err != nil {
		return 0, err
	}
	if w.shown {
		return len(p), nil
	}

	w.buf += string(p)
	if url := authURLPattern.FindString(w.buf); url != "" {
		w.shown = true
		renderAuthQR(w.out, url)
	}

	// Avoid retaining unlimited gcloud output before the URL appears.
	if len(w.buf) > 64*1024 {
		w.buf = w.buf[len(w.buf)-4096:]
	}
	return len(p), nil
}

func renderAuthQR(out io.Writer, url string) {
	fmt.Fprintln(out)
	qrterminal.GenerateWithConfig(url, qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     out,
		HalfBlocks: true,
		QuietZone:  1,
	})
	fmt.Fprintln(out)
	fmt.Fprintln(out, url)
	fmt.Fprintln(out)
}

func runRemoteAuth() error {
	cmd := exec.Command("gcloud", "auth", "login", "--no-launch-browser")
	writer := &authWriter{out: os.Stdout}
	cmd.Stdout = writer
	cmd.Stderr = writer
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__remote-auth" {
		if err := runRemoteAuth(); err != nil {
			fmt.Fprintln(os.Stderr, "remote login failed:", err)
			os.Exit(1)
		}
		return
	}

	m := initialModel()
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "cloud:", err)
		os.Exit(1)
	}
}
