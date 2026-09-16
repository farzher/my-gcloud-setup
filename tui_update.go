package main

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"strings"
)

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.PasteMsg:
		text := strings.TrimSpace(msg.Content)
		if m.editingDomain {
			var changed bool
			m.domainInput, m.domainCursor, changed = insertSingleLine(m.domainInput, m.domainCursor, text, 253)
			if changed {
				m.domainError = ""
			}
		} else if m.editingSite {
			var changed bool
			m.siteInput, m.siteCursor, changed = insertSingleLine(m.siteInput, m.siteCursor, text, 80)
			if changed {
				m.siteError = ""
			}
		}
		return m, nil
	case tickMsg:
		m.frame++
		if m.screen == screenBilling && !m.busy && !m.refreshing && m.frame%40 == 0 {
			m.busy = true
			return m, tea.Batch(tick(), detectCmd(m.cfg))
		}
		return m, tick()
	case detectedMsg:
		m.busy = false
		if msg.err != nil {
			m.refreshing = false
			back := m.screen
			if back == screenDetails {
				back = screenLoading
			}
			return m.showError(back, msg.err, msg.err.Error())
		}

		oldAccount := m.state.Account
		if oldAccount == msg.state.Account && oldAccount != "" {
			m.state.Gcloud = msg.state.Gcloud
			m.state.Account = msg.state.Account
			m.state.Accounts = msg.state.Accounts
		} else {
			m.state = msg.state
			if oldAccount != m.state.Account {
				m.resetAccountTransient()
			}
		}
		m.cfg.Account = m.state.Account
		m.cfg.Project = m.cfg.projectFor(m.state.Account)
		m.cfg.Repo = m.cfg.repoFor(m.state.Account)

		if !m.state.Gcloud {
			m.refreshing = false
			m.statusText = ""
			m.screen = screenNeedGcloud
			return m, nil
		}
		if m.state.Account == "" {
			m.refreshing = false
			m.statusText = ""
			m.screen = screenAccount
			m.accountPos = 0
			return m, nil
		}

		m.refreshing = true
		m.statusText = "Checking cloud"
		if m.screen == screenLoading || oldAccount != m.state.Account {
			m.screen = screenServer
			if strings.TrimSpace(m.cfg.nameFor(m.state.Account)) == "" {
				m.editingSite = true
				m.siteInput, m.siteError = "", ""
				m.siteCursor = 0
			} else if m.cfg.regionFor(m.state.Account) == "" {
				m.screen = screenLocation
				m.locationPos = 0
			}
		}
		return m, fullDetectCmd(m.cfg, m.state.Account)
	case fullDetectedMsg:
		if msg.account != m.state.Account {
			return m, nil
		}
		m.refreshing = false
		m.statusText = ""
		if msg.err != nil {
			return m.showError(m.screen, msg.err, msg.err.Error())
		}
		if msg.state.Account != msg.account {
			m.busy = true
			return m, detectCmd(m.cfg)
		}

		m.state = msg.state
		m.cfg.Account = m.state.Account
		m.cfg.Project = m.cfg.projectFor(m.state.Account)
		m.cfg.Repo = m.cfg.repoFor(m.state.Account)
		if m.billingID == "" {
			remembered := m.cfg.billingFor(m.state.Account)
			if billingHas(m.state.Billing, remembered) {
				m.billingID = remembered
			}
		}
		if m.cfg.Project == "" && m.state.ManagedProject != "" {
			m.cfg.setProject(m.state.Account, m.state.ManagedProject)
			m.cfg.Project = m.state.ManagedProject
			if err := saveConfig(m.cfg); err != nil {
				return m.showError(screenLoading, err, err.Error())
			}
		}
		if m.billingID != "" && !billingHas(m.state.Billing, m.billingID) {
			m.billingID, m.billingPos = "", 0
		}
		m.syncMenu()

		var cmds []tea.Cmd
		if m.screen != screenAccount && m.screen != screenConfirm && m.screen != screenDetails && !m.editingSite && !m.editingDomain {
			if cmd := m.route(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.state.Account != "" && !m.vmScanBusy && m.vmScanAccount != m.state.Account && !m.state.VMExists {
			m.vmScanBusy = true
			cmds = append(cmds, scanVMsCmd(m.state.Account, m.cfg.Project, m.cfg.zone()))
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	case vmScanMsg:
		m.vmScanBusy = false
		if msg.account != m.state.Account {
			return m, nil
		}
		m.vmScanAccount, m.otherVMs, m.otherVMCount = msg.account, msg.vms, msg.count
		return m, m.route()
	case stepDoneMsg:
		m.busy = false
		m.cfg = msg.cfg
		m.lastOutput, m.lastCommand = msg.output, msg.command
		if msg.index < 0 || msg.index >= len(m.steps) {
			return m, nil
		}
		if msg.err != nil {
			if errors.Is(msg.err, errChatGPTAuthRequired) {
				m.steps[msg.index].State = 1
				m.statusText = "ChatGPT login"
				return m, chatGPTAuthCmd(m.cfg)
			}
			if errors.Is(msg.err, errGitHubAuthRequired) {
				m.steps[msg.index].State = 1
				m.statusText = "GitHub login"
				return m, githubAuthCmd()
			}
			if errors.Is(msg.err, errDNSRequired) && m.cfg.domainFor(m.state.Account) != "" {
				m.cfg.setHTTPSDeferred(m.state.Account, true)
				if err := saveConfig(m.cfg); err != nil {
					return m.showError(screenServer, err, err.Error())
				}
				m.lastErr = nil
				m.steps[msg.index].State = 2
				m.steps[msg.index].Detail = "pending DNS"
				if msg.index == 12 && len(m.steps) > 13 {
					m.steps[13].State = 2
					m.steps[13].Detail = "deferred"
				}
				m.stepIndex = 14
				m.steps[m.stepIndex].State = 1
				m.busy = true
				return m, runStepCmd(m.stepIndex, m.cfg, m.billingID)
			}
			m.steps[msg.index].State = 3
			detail := shortError(msg.err)
			for _, line := range nonEmptyLines(msg.output) {
				if strings.Contains(strings.ToUpper(line), "ERROR") {
					detail = shortError(errors.New(line))
					break
				}
			}
			m.steps[msg.index].Detail = detail
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
		return m, runStepCmd(m.stepIndex, m.cfg, m.billingID)
	case authDoneMsg:
		m.busy = true
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
		}
		return m, detectCmd(m.cfg)
	case actionDoneMsg:
		m.busy = false
		m.cfg = msg.cfg
		if msg.err != nil {
			return m.showError(screenServer, msg.err, msg.output)
		}
		m.statusText = msg.name + " complete"
		m.busy = true
		return m, detectCmd(m.cfg)
	case rebuildReadyMsg:
		m.cfg = msg.cfg
		m.billingID = msg.billingID
		m.confirm = confirmNone
		m.startProvisionAt(5)
		return m, runStepCmd(5, m.cfg, m.billingID)
	case renameDoneMsg:
		m.busy = false
		if msg.err != nil {
			return m.showError(screenServer, msg.err, msg.output)
		}
		m.cfg = msg.cfg
		m.editingSite, m.siteInput, m.siteError = false, "", ""
		m.siteCursor = 0
		m.busy = true
		return m, detectCmd(m.cfg)
	case browserDoneMsg:
		if msg.err != nil {
			return m.showError(m.screen, msg.err, msg.err.Error())
		}
		return m, nil
	case billingActionMsg:
		if msg.err != nil {
			return m.showError(screenBilling, msg.err, msg.err.Error())
		}
		m.statusText = msg.status
		if msg.refresh {
			m.busy = true
			return m, detectCmd(m.cfg)
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	k := key.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.editingDomain {
		return m.updateDomainInput(key)
	}
	if m.editingSite {
		return m.updateSiteInput(key)
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
	case screenAccount:
		return m.updateAccount(k)
	case screenBilling:
		switch k {
		case "up", "k":
			m.billingSetupPos--
			if m.billingSetupPos < 0 {
				m.billingSetupPos = 2
			}
		case "down", "j", "tab":
			m.billingSetupPos = (m.billingSetupPos + 1) % 3
		case "enter":
			switch m.billingSetupPos {
			case 0:
				return m, copyBillingLinkCmd(m.state.Account)
			case 1:
				return m, billingQRCmd(m.state.Account)
			case 2:
				m.statusText = "Opening billing"
				return m, openBrowserCmd(billingSetupURL(m.state.Account))
			}
		case "o":
			m.statusText = "Opening billing"
			return m, openBrowserCmd(billingSetupURL(m.state.Account))
		case "r":
			m.busy = true
			return m, detectCmd(m.cfg)
		case "a":
			m.screen = screenAccount
			m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
		case "q":
			return m, tea.Quit
		}
	case screenLocation:
		switch k {
		case "up", "k":
			m.locationPos--
			if m.locationPos < 0 {
				m.locationPos = len(freeLocations) - 1
			}
		case "down", "j", "tab":
			m.locationPos = (m.locationPos + 1) % len(freeLocations)
		case "enter":
			loc := freeLocations[m.locationPos]
			m.cfg.setRegion(m.state.Account, loc.Region)
			if err := saveConfig(m.cfg); err != nil {
				return m.showError(screenLocation, err, err.Error())
			}
			return m, m.route()
		case "a":
			m.screen = screenAccount
			m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
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
			if m.billingPos < len(m.state.Billing)-1 {
				m.billingPos++
			}
		case "enter":
			if len(m.state.Billing) > 0 {
				m.billingID = m.state.Billing[m.billingPos].id()
				m.cfg.setBilling(m.state.Account, m.billingID)
				if err := saveConfig(m.cfg); err != nil {
					return m.showError(screenBillingPick, err, err.Error())
				}
				return m, m.route()
			}
		case "q":
			return m, tea.Quit
		}
	case screenServer:
		if m.refreshing {
			switch k {
			case "a":
				m.screen = screenAccount
				m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
			case "q":
				return m, tea.Quit
			}
			return m, nil
		}
		return m.updateServer(k)
	case screenConfirm:
		return m.updateConfirm(k)
	case screenDetails:
		switch k {
		case "enter", "esc", "q":
			m.screen = m.returnScreen
		}
	}
	return m, nil
}
