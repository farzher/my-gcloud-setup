package main

import (
	tea "charm.land/bubbletea/v2"
	"errors"
)

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
			return m.showError(screenLoading, msg.err, msg.err.Error())
		}
		oldAccount := m.state.Account
		m.state = msg.state
		if oldAccount != m.state.Account {
			m.resetAccountTransient()
		}
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
			m.busy = true
			return m, detectCmd(m.cfg)
		}
		if m.billingID != "" && !billingHas(m.state.Billing, m.billingID) {
			m.billingID, m.billingPos = "", 0
		}
		m.syncMenu()
		cmd := m.route()
		var cmds []tea.Cmd
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if m.state.Account != "" && !m.vmScanBusy && m.vmScanAccount != m.state.Account && !m.state.VMExists {
			m.vmScanBusy = true
			cmds = append(cmds, scanVMsCmd(m.state.Account, m.cfg.Project))
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
		return m, runStepCmd(m.stepIndex, m.cfg, m.billingID)
	case authDoneMsg:
		m.busy = true
		if msg.err != nil {
			m.lastErr = msg.err
			m.lastOutput = msg.err.Error()
		}
		return m, detectCmd(m.cfg)
	case externalDoneMsg:
		m.busy = true
		if msg.err != nil {
			return m.showError(screenServer, msg.err, msg.err.Error())
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
		m.busy = true
		return m, detectCmd(m.cfg)
	case browserDoneMsg:
		if msg.err != nil {
			return m.showError(m.screen, msg.err, msg.err.Error())
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
