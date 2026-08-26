package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"errors"
	"fmt"
	"strings"
)

func (m model) renderServer() string {
	var b strings.Builder
	b.WriteString(m.header() + "\n\n" + titleStyle.Render("Server"))
	name := m.cfg.nameFor(m.state.Account)
	if name != "" && !m.editingSite {
		b.WriteString("  " + mutedStyle.Render(name))
	}
	if m.state.Account != "" {
		b.WriteString("\n" + mutedStyle.Render(m.state.Account))
	}
	b.WriteString("\n\n")

	if m.editingSite {
		b.WriteString(titleStyle.Render("Domain / name") + "\n\n")
		b.WriteString(accentStyle.Render("› ") + m.siteInput + accentStyle.Render("▌"))
		if m.siteError != "" {
			b.WriteString("\n\n" + badStyle.Render(m.siteError))
		}
		b.WriteString("\n\n" + mutedStyle.Render("enter"))
		if m.cfg.Project != "" {
			b.WriteString(mutedStyle.Render("  esc"))
		}
		return b.String()
	}

	if (!m.state.VMExists && len(m.steps) == 0) || m.cfg.disabledFor(m.state.Account) {
		if m.vmScanAccount != m.state.Account {
			b.WriteString(spinner(m.frame) + " instances")
			return b.String()
		}
		if m.otherVMCount > 0 && !m.vmWarningAck {
			label := fmt.Sprintf("⚠ %d other VM", m.otherVMCount)
			if m.otherVMCount != 1 {
				label += "s"
			}
			b.WriteString(warnStyle.Render(label))
			if len(m.otherVMs) > 0 {
				b.WriteString("\n" + mutedStyle.Render(m.otherVMs[0].Project+"/"+m.otherVMs[0].Name))
			}
			b.WriteString("\n\n" + mutedStyle.Render("enter acknowledge  a account  q"))
			return b.String()
		}
		b.WriteString(button("Create") + "\n\n" + mutedStyle.Render("enter  a account  q"))
		return b.String()
	}

	if len(m.steps) > 0 {
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
			line := fmt.Sprintf("%s %-12s", icon, s.Name)
			if s.Detail != "" {
				line += " " + mutedStyle.Render(s.Detail)
			}
			b.WriteString(line + "\n")
		}
		if !m.busy && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
			b.WriteString("\n" + badStyle.Render(shortError(m.lastErr)))
			if errors.Is(m.lastErr, errDNSRequired) {
				b.WriteString("\n" + warnStyle.Render("A "+m.cfg.domainFor(m.state.Account)+" → "+m.state.StaticIP))
			}
			b.WriteString("\n\n" + mutedStyle.Render("r retry  d details  q"))
		}
		return b.String()
	}

	status := strings.ToUpper(m.state.Instance.Status)
	if status == "" {
		status = "—"
	}
	ip := m.state.Instance.ip()
	if ip == "" {
		ip = m.state.StaticIP
	}
	rows := []struct {
		name, detail string
		ok           bool
	}{
		{"VM", status, m.state.VMExists},
		{"IP", ip, ip != ""},
		{"Hermes", chatGPTModel + " · " + chatGPTEffort, m.state.ChatGPTReady},
		{"GitHub", m.cfg.repoFor(m.state.Account), m.state.GitHubReady},
	}
	domain := m.cfg.domainFor(m.state.Account)
	if domain != "" {
		rows = append(rows, struct {
			name, detail string
			ok           bool
		}{"HTTPS", domain, m.state.HTTPSReady})
	} else {
		rows = append(rows, struct {
			name, detail string
			ok           bool
		}{"Web", "HTTP", m.state.WebReady})
	}
	for _, r := range rows {
		icon := mutedStyle.Render("·")
		if r.ok {
			icon = goodStyle.Render("✓")
		}
		b.WriteString(fmt.Sprintf("%s %-8s", icon, r.name))
		if r.detail != "" {
			b.WriteString(" " + mutedStyle.Render(r.detail))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for i, item := range m.menu {
		b.WriteString(choiceLine(item, i == m.menuPos) + "\n")
	}
	if m.statusText != "" {
		b.WriteString("\n" + spinner(m.frame) + " " + m.statusText)
	}
	b.WriteString("\n" + mutedStyle.Render("↑/↓  enter  r  n  q"))
	return b.String()
}

func (m model) renderConfirm() string {
	var title string
	var options []string
	if m.confirm == confirmRebuild {
		title = "Rebuild?"
		options = []string{"Rebuild · keep IP/repo", "Cancel"}
	} else {
		title = "Destroy?"
		options = []string{"VM · keep IP/repo", "VM + IP · keep repo", "Cancel"}
	}
	var b strings.Builder
	b.WriteString(m.header() + "\n\n" + badStyle.Render(title) + "\n\n")
	for i, s := range options {
		b.WriteString(choiceLine(s, i == m.confirmPos) + "\n")
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
		b.WriteString(badStyle.Render("Error"))
	} else {
		b.WriteString(titleStyle.Render("Details"))
	}
	if m.lastCommand != "" {
		b.WriteString("\n\n" + mutedStyle.Render("$ "+m.lastCommand))
	}
	b.WriteString("\n\n" + strings.Join(lines, "\n") + "\n\n" + mutedStyle.Render("enter/esc"))
	return b.String()
}

func (m model) showError(back screen, err error, output string) (tea.Model, tea.Cmd) {
	m.busy = false
	m.lastErr, m.lastOutput, m.returnScreen = err, output, back
	m.screen = screenDetails
	return m, nil
}

func (m *model) resetAccountTransient() {
	m.billingID, m.billingPos, m.accountPos = "", 0, 0
	m.otherVMs, m.otherVMCount, m.vmScanAccount = nil, 0, ""
	m.vmScanBusy, m.vmWarningAck = false, false
	m.editingSite, m.siteInput, m.siteError = false, "", ""
	m.steps, m.stepIndex = nil, 0
	m.lastErr, m.lastOutput, m.lastCommand = nil, "", ""
}

func choiceLine(label string, active bool) string {
	if active {
		return accentStyle.Render("› " + label)
	}
	return "  " + label
}

func button(label string) string {
	return lipgloss.NewStyle().Padding(0, 2).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Foreground(bright).Bold(true).Render(label)
}

func spinner(frame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return accentStyle.Render(frames[frame%len(frames)])
}

func activeAccountPos(accounts []string, active string) int {
	for i, a := range accounts {
		if a == active {
			return i
		}
	}
	return 0
}
