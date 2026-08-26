package main

import (
	tea "charm.land/bubbletea/v2"
	"strings"
)

func (m model) updateSiteInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	switch k {
	case "enter":
		name, domain, err := parseSite(m.siteInput)
		if err != nil {
			m.siteError = err.Error()
			return m, nil
		}
		m.siteError = ""
		if m.cfg.Project == "" {
			m.cfg.setSite(m.state.Account, name, domain)
			if err = saveConfig(m.cfg); err != nil {
				return m.showError(screenServer, err, err.Error())
			}
			m.editingSite = false
			return m, m.route()
		}
		m.busy = true
		return m, renameSiteCmd(m.cfg, name, domain)
	case "esc":
		if m.cfg.Project != "" {
			m.editingSite, m.siteInput, m.siteError = false, "", ""
		}
		return m, nil
	case "backspace":
		r := []rune(m.siteInput)
		if len(r) > 0 {
			m.siteInput = string(r[:len(r)-1])
		}
		m.siteError = ""
		return m, nil
	default:
		text := key.Key().Text
		if text != "" && len([]rune(m.siteInput+text)) <= 80 {
			m.siteInput += text
			m.siteError = ""
		}
		return m, nil
	}
}

func (m model) updateAccount(k string) (tea.Model, tea.Cmd) {
	count := len(m.state.Accounts) + 2
	if count < 2 {
		count = 2
	}
	switch k {
	case "up", "k":
		if m.accountPos > 0 {
			m.accountPos--
		}
	case "down", "j", "tab":
		m.accountPos++
		if m.accountPos >= count {
			m.accountPos = 0
		}
	case "q":
		return m, tea.Quit
	case "enter":
		if m.accountPos < len(m.state.Accounts) {
			m.busy = true
			return m, switchGoogleAccountCmd(m.state.Accounts[m.accountPos])
		}
		if m.accountPos == len(m.state.Accounts) {
			m.busy = true
			return m, googleBrowserAuthCmd()
		}
		m.busy = true
		return m, googleQRAuthCmd()
	}
	return m, nil
}

func (m model) updateServer(k string) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	if (!m.state.VMExists && len(m.steps) == 0) || m.cfg.disabledFor(m.state.Account) {
		switch k {
		case "enter":
			if m.vmScanAccount != m.state.Account {
				return m, nil
			}
			if m.otherVMCount > 0 && !m.vmWarningAck {
				m.vmWarningAck = true
				return m, nil
			}
			m.cfg.setDisabled(m.state.Account, false)
			_ = saveConfig(m.cfg)
			m.startProvisionAt(0)
			return m, runStepCmd(0, m.cfg, m.billingID)
		case "a":
			m.screen = screenAccount
			m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
		case "q":
			return m, tea.Quit
		}
		return m, nil
	}
	if len(m.steps) > 0 && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
		switch k {
		case "r", "enter":
			m.steps[m.stepIndex].State = 1
			m.busy = true
			return m, runStepCmd(m.stepIndex, m.cfg, m.billingID)
		case "d":
			m.returnScreen = screenServer
			m.screen = screenDetails
		case "q":
			return m, tea.Quit
		}
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
	case "n":
		m.editingSite = true
		if m.cfg.domainFor(m.state.Account) != "" {
			m.siteInput = m.cfg.domainFor(m.state.Account)
		} else {
			m.siteInput = m.cfg.nameFor(m.state.Account)
		}
	case "q":
		return m, tea.Quit
	case "enter":
		return m.activateMenu()
	}
	return m, nil
}

func (m model) updateConfirm(k string) (tea.Model, tea.Cmd) {
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
		m.screen, m.confirm = screenServer, confirmNone
	case "enter":
		return m.runConfirmed()
	}
	return m, nil
}

func (m *model) route() tea.Cmd {
	if !m.state.Gcloud {
		m.screen = screenNeedGcloud
		return nil
	}
	if m.state.Account == "" {
		m.screen = screenAccount
		m.accountPos = 0
		return nil
	}
	if len(m.state.Billing) == 0 {
		m.screen = screenBilling
		return nil
	}
	if len(m.state.Billing) == 1 {
		m.billingID = m.state.Billing[0].id()
		if m.cfg.billingFor(m.state.Account) != m.billingID {
			m.cfg.setBilling(m.state.Account, m.billingID)
			_ = saveConfig(m.cfg)
		}
	} else if m.billingID == "" {
		m.screen = screenBillingPick
		return nil
	}
	m.screen = screenServer
	if strings.TrimSpace(m.cfg.nameFor(m.state.Account)) == "" {
		m.editingSite = true
		m.siteInput, m.siteError = "", ""
		return nil
	}
	if m.cfg.disabledFor(m.state.Account) {
		m.steps = nil
		return nil
	}
	if !m.state.VMExists {
		if m.vmScanAccount != m.state.Account || (m.otherVMCount > 0 && !m.vmWarningAck) {
			return nil
		}
		m.startProvisionAt(0)
		return runStepCmd(0, m.cfg, m.billingID)
	}
	if !strings.EqualFold(m.state.Instance.Status, "RUNNING") {
		m.steps = nil
		return nil
	}
	idx := firstMissingStep(m.state, m.cfg)
	if idx >= 0 {
		m.startProvisionAt(idx)
		return runStepCmd(idx, m.cfg, m.billingID)
	}
	m.steps = nil
	return nil
}

func firstMissingStep(s cloudState, cfg config) int {
	checks := []bool{
		s.ProjectOK,
		true,
		true,
		true,
		s.StaticIP != "" && (!s.VMExists || s.Instance.ip() == s.StaticIP),
		s.VMExists,
		s.SSHReady,
		s.SystemReady,
		s.HermesReady,
		s.ChatGPTReady,
		s.GitHubReady,
		s.WebReady,
		cfg.domainFor(s.Account) == "" || s.DNSReady,
		cfg.domainFor(s.Account) == "" || s.HTTPSReady,
		s.VerifyReady,
	}
	for i, ok := range checks {
		if !ok {
			return i
		}
	}
	return -1
}

func (m *model) startProvisionAt(index int) {
	m.screen = screenServer
	m.busy = true
	m.lastErr, m.lastOutput, m.lastCommand = nil, "", ""
	m.steps = makeSteps(m.cfg.domainFor(m.state.Account) != "")
	if index < 0 {
		index = 0
	}
	if index >= len(m.steps) {
		index = len(m.steps) - 1
	}
	for i := 0; i < index; i++ {
		m.steps[i].State = 2
	}
	m.stepIndex = index
	m.steps[index].State = 1
}

func makeSteps(hasDomain bool) []provisionStep {
	steps := []provisionStep{
		{Name: "Project"}, {Name: "Billing"}, {Name: "Compute"}, {Name: "Network"},
		{Name: "Static IP"}, {Name: "VM"}, {Name: "SSH"}, {Name: "System"},
		{Name: "Hermes"}, {Name: "ChatGPT"}, {Name: "GitHub"}, {Name: "Web"},
		{Name: "DNS"}, {Name: "HTTPS"}, {Name: "Ready"},
	}
	if !hasDomain {
		steps[12].Detail, steps[13].Detail = "skip", "skip"
	}
	return steps
}
