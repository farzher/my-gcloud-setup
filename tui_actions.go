package main

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"strings"
	"unicode"
)

func editSingleLine(value string, cursor int, key tea.KeyPressMsg, maxLen int) (string, int, bool) {
	r := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}

	switch key.String() {
	case "left":
		if cursor > 0 {
			cursor--
		}
		return value, cursor, false
	case "right":
		if cursor < len(r) {
			cursor++
		}
		return value, cursor, false
	case "home":
		return value, 0, false
	case "end":
		return value, len(r), false
	case "backspace":
		if cursor == 0 {
			return value, cursor, false
		}
		r = append(r[:cursor-1], r[cursor:]...)
		return string(r), cursor - 1, true
	case "delete":
		if cursor >= len(r) {
			return value, cursor, false
		}
		r = append(r[:cursor], r[cursor+1:]...)
		return string(r), cursor, true
	}

	text := key.Key().Text
	if text == "" {
		return value, cursor, false
	}
	for _, c := range text {
		if unicode.IsControl(c) {
			return value, cursor, false
		}
	}
	insert := []rune(text)
	if len(r)+len(insert) > maxLen {
		return value, cursor, false
	}
	r = append(r, make([]rune, len(insert))...)
	copy(r[cursor+len(insert):], r[cursor:len(r)-len(insert)])
	copy(r[cursor:], insert)
	return string(r), cursor + len(insert), true
}

func insertSingleLine(value string, cursor int, text string, maxLen int) (string, int, bool) {
	for _, c := range text {
		if unicode.IsControl(c) {
			return value, cursor, false
		}
	}
	r := []rune(value)
	insert := []rune(text)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}
	if len(r)+len(insert) > maxLen {
		return value, cursor, false
	}
	r = append(r, make([]rune, len(insert))...)
	copy(r[cursor+len(insert):], r[cursor:len(r)-len(insert)])
	copy(r[cursor:], insert)
	return string(r), cursor + len(insert), len(insert) > 0
}

func (m model) updateSiteInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		name, domain, err := parseSite(m.siteInput)
		if err != nil {
			m.siteError = err.Error()
			return m, nil
		}
		m.siteError = ""
		if m.cfg.Project == "" {
			m.cfg.setSite(m.state.Account, name, domain)
			m.cfg.setHTTPSDeferred(m.state.Account, false)
			if err = saveConfig(m.cfg); err != nil {
				return m.showError(screenServer, err, err.Error())
			}
			m.editingSite = false
			m.siteCursor = 0
			return m, m.route()
		}
		m.busy = true
		return m, renameSiteCmd(m.cfg, name, domain)
	case "esc":
		m.editingSite, m.siteInput, m.siteError = false, "", ""
		m.siteCursor = 0
		if m.cfg.Project == "" {
			m.screen = screenAccount
			m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
		}
		return m, nil
	}

	var changed bool
	m.siteInput, m.siteCursor, changed = editSingleLine(m.siteInput, m.siteCursor, key, 80)
	if changed {
		m.siteError = ""
	}
	return m, nil
}

func (m model) updateDomainInput(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		_, domain, err := parseSite(m.domainInput)
		if err != nil || domain == "" {
			m.domainError = "enter a domain like example.com"
			return m, nil
		}
		m.cfg.setDomain(m.state.Account, domain)
		m.cfg.setHTTPSDeferred(m.state.Account, false)
		if err = saveConfig(m.cfg); err != nil {
			return m.showError(screenServer, err, err.Error())
		}
		m.editingDomain, m.domainInput, m.domainError = false, "", ""
		m.domainCursor = 0
		m.startProvisionAt(11)
		return m, runStepCmd(11, m.cfg, m.billingID)
	case "esc":
		m.editingDomain, m.domainInput, m.domainError = false, "", ""
		m.domainCursor = 0
		return m, nil
	}

	var changed bool
	m.domainInput, m.domainCursor, changed = editSingleLine(m.domainInput, m.domainCursor, key, 253)
	if changed {
		m.domainError = ""
	}
	return m, nil
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
			target := m.state.Accounts[m.accountPos]
			if target == m.state.Account {
				m.screen = screenServer
				return m, m.route()
			}
			m.busy = true
			m.screen = screenLoading
			m.statusText = "Switching account"
			return m, switchGoogleAccountCmd(target)
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
	if m.state.VMExists && len(m.steps) == 0 && strings.EqualFold(m.state.Instance.Status, "RUNNING") {
		if idx := firstMissingStep(m.state, m.cfg); idx >= 0 {
			switch k {
			case "enter":
				m.startProvisionAt(idx)
				return m, runStepCmd(idx, m.cfg, m.billingID)
			case "a":
				m.screen = screenAccount
				m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
			case "r":
				m.busy = true
				m.statusText = "Refreshing"
				return m, detectCmd(m.cfg)
			case "q":
				return m, tea.Quit
			}
			return m, nil
		}
	}
	if len(m.steps) > 0 && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
		switch k {
		case "r", "enter":
			m.steps[m.stepIndex].State = 1
			m.busy = true
			return m, runStepCmd(m.stepIndex, m.cfg, m.billingID)
		case "s":
			if m.cfg.domainFor(m.state.Account) != "" && (errors.Is(m.lastErr, errDNSRequired) || m.stepIndex == 12 || m.stepIndex == 13) {
				m.cfg.setHTTPSDeferred(m.state.Account, true)
				if err := saveConfig(m.cfg); err != nil {
					return m.showError(screenServer, err, err.Error())
				}
				m.startProvisionAt(14)
				m.steps[12].Detail = "HTTP for now"
				m.steps[13].Detail = "deferred"
				return m, runStepCmd(14, m.cfg, m.billingID)
			}
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
		m.siteCursor = 0
		return nil
	}
	if m.cfg.regionFor(m.state.Account) == "" {
		m.screen = screenLocation
		m.locationPos = 0
		return nil
	}
	if m.cfg.disabledFor(m.state.Account) {
		m.steps = nil
		return nil
	}
	if !m.state.VMExists {
		m.steps = nil
		return nil
	}
	if !strings.EqualFold(m.state.Instance.Status, "RUNNING") {
		m.steps = nil
		return nil
	}
	if firstMissingStep(m.state, m.cfg) >= 0 {
		m.steps = nil
		return nil
	}
	m.steps = nil
	return nil
}

func firstMissingStep(s cloudState, cfg config) int {
	domainOptional := cfg.domainFor(s.Account) == "" || cfg.httpsDeferredFor(s.Account)
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
		domainOptional || s.DNSReady,
		domainOptional || s.HTTPSReady,
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
