package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"time"
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
	githubOwner = "farzher"

	billingURL = "https://console.cloud.google.com/billing/create"
	gcloudURL  = "https://cloud.google.com/sdk/docs/install"
)

type screen int

const (
	screenLoading screen = iota
	screenNeedGcloud
	screenAccount
	screenBilling
	screenBillingPick
	screenServer
	screenConfirm
	screenDetails
)

type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmRebuild
	confirmDestroy
)

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

	cfg   config
	state cloudState

	billingID  string
	billingPos int
	accountPos int

	otherVMs      []existingVM
	otherVMCount  int
	vmScanAccount string
	vmScanBusy    bool
	vmWarningAck  bool

	editingSite bool
	siteInput   string
	siteError   string

	steps     []provisionStep
	stepIndex int
	busy      bool

	menu    []string
	menuPos int

	confirm    confirmKind
	confirmPos int

	lastErr      error
	lastOutput   string
	lastCommand  string
	returnScreen screen
	statusText   string
}

type tickMsg time.Time

type detectedMsg struct {
	state cloudState
	err   error
}

type vmScanMsg struct {
	account string
	vms     []existingVM
	count   int
}

type stepDoneMsg struct {
	index   int
	cfg     config
	detail  string
	output  string
	command string
	err     error
}

type authDoneMsg struct{ err error }

type externalDoneMsg struct{ err error }

type actionDoneMsg struct {
	name   string
	cfg    config
	output string
	err    error
}

type rebuildReadyMsg struct {
	cfg       config
	billingID string
}

type renameDoneMsg struct {
	cfg    config
	output string
	err    error
}

type browserDoneMsg struct{ err error }

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
	m := model{screen: screenLoading, cfg: loadConfig()}
	m.syncMenu()
	return m
}

func (m model) Init() tea.Cmd { return tea.Batch(detectCmd(m.cfg), tick()) }

func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
