package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"strings"
)

func (m *model) syncMenu() {
	power := "Stop"
	if strings.EqualFold(m.state.Instance.Status, "TERMINATED") || strings.EqualFold(m.state.Instance.Status, "STOPPED") {
		power = "Start"
	}
	m.menu = []string{"Hermes", "Gateway", "SSH", "Restart", power, "Rebuild", "Destroy", "Account"}
	if m.menuPos >= len(m.menu) {
		m.menuPos = max(0, len(m.menu)-1)
	}
}

func (m model) activateMenu() (tea.Model, tea.Cmd) {
	if m.menuPos < 0 || m.menuPos >= len(m.menu) {
		return m, nil
	}
	switch m.menu[m.menuPos] {
	case "Hermes":
		return m, remoteHermesCmd(m.cfg)
	case "Gateway":
		return m, remoteGatewayCmd(m.cfg)
	case "SSH":
		return m, remoteSSHCmd(m.cfg)
	case "Restart":
		m.busy, m.statusText = true, "Restarting"
		return m, lifecycleCmd("Restart", m.cfg, "reset")
	case "Stop":
		m.busy, m.statusText = true, "Stopping"
		return m, lifecycleCmd("Stop", m.cfg, "stop")
	case "Start":
		m.busy, m.statusText = true, "Starting"
		return m, lifecycleCmd("Start", m.cfg, "start")
	case "Rebuild":
		m.screen, m.confirm, m.confirmPos = screenConfirm, confirmRebuild, 1
	case "Destroy":
		m.screen, m.confirm, m.confirmPos = screenConfirm, confirmDestroy, 2
	case "Account":
		m.screen = screenAccount
		m.accountPos = activeAccountPos(m.state.Accounts, m.state.Account)
	}
	return m, nil
}

func (m model) runConfirmed() (tea.Model, tea.Cmd) {
	switch m.confirm {
	case confirmRebuild:
		if m.confirmPos == 0 {
			m.screen, m.busy = screenServer, true
			m.statusText = "Rebuilding"
			return m, rebuildCmd(m.cfg, m.billingID)
		}
	case confirmDestroy:
		switch m.confirmPos {
		case 0:
			m.screen, m.busy = screenServer, true
			return m, destroyCmd(m.cfg, false)
		case 1:
			m.screen, m.busy = screenServer, true
			return m, destroyCmd(m.cfg, true)
		}
	}
	m.screen, m.confirm = screenServer, confirmNone
	return m, nil
}

func (m model) View() tea.View {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	boxWidth := 50
	if w < boxWidth+4 {
		boxWidth = max(34, w-4)
	}
	card := lipgloss.NewStyle().Width(boxWidth).Padding(1, 2).
		Border(lipgloss.RoundedBorder()).BorderForeground(accent).Render(m.render())
	v := tea.NewView(lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, card))
	v.AltScreen = true
	return v
}

func (m model) render() string {
	switch m.screen {
	case screenLoading:
		return m.header() + "\n\n" + spinner(m.frame) + " cloud"
	case screenNeedGcloud:
		return m.header() + "\n\n" + badStyle.Render("gcloud not found") + "\n\n" + button("Install gcloud") + "\n\n" + mutedStyle.Render("enter  r  q")
	case screenAccount:
		return m.renderAccount()
	case screenBilling:
		return m.header() + "\n\n" + titleStyle.Render("Billing") + "\n" + mutedStyle.Render(m.state.Account) + "\n\n" + button("Open billing") + "\n\n" + mutedStyle.Render("enter  r  q")
	case screenBillingPick:
		return m.renderBilling()
	case screenServer:
		return m.renderServer()
	case screenConfirm:
		return m.renderConfirm()
	case screenDetails:
		return m.renderDetails()
	default:
		return m.header()
	}
}

func (m model) header() string { return accentStyle.Render("●") + " " + titleStyle.Render(appName) }

func (m model) renderAccount() string {
	var b strings.Builder
	b.WriteString(m.header() + "\n\n" + titleStyle.Render("Account") + "\n\n")
	for i, a := range m.state.Accounts {
		label := a
		if a == m.state.Account {
			label += "  ✓"
		}
		b.WriteString(choiceLine(label, i == m.accountPos) + "\n")
	}
	base := len(m.state.Accounts)
	b.WriteString(choiceLine("Browser", m.accountPos == base) + "\n")
	b.WriteString(choiceLine("QR code", m.accountPos == base+1) + "\n\n")
	b.WriteString(mutedStyle.Render("↑/↓  enter  q"))
	return b.String()
}

func (m model) renderBilling() string {
	var b strings.Builder
	b.WriteString(m.header() + "\n\n" + titleStyle.Render("Billing") + "\n\n")
	for i, a := range m.state.Billing {
		name := a.DisplayName
		if name == "" {
			name = a.id()
		}
		b.WriteString(choiceLine(name, i == m.billingPos) + "\n")
	}
	b.WriteString("\n" + mutedStyle.Render("↑/↓  enter  q"))
	return b.String()
}
