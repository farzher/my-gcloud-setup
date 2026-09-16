package main

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"errors"
	"fmt"
	"strings"
	"time"
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

	if m.editingDomain {
		b.WriteString(titleStyle.Render("Domain") + "\n\n")
		b.WriteString(accentStyle.Render("› ") + m.domainInput + accentStyle.Render("▌"))
		if m.domainError != "" {
			b.WriteString("\n\n" + badStyle.Render(m.domainError))
		}
		b.WriteString("\n\n" + mutedStyle.Render("enter save + configure HTTPS  esc"))
		return b.String()
	}

	if m.editingSite {
		b.WriteString(titleStyle.Render("Domain / name") + "\n\n")
		b.WriteString(accentStyle.Render("› ") + m.siteInput + accentStyle.Render("▌"))
		if m.siteError != "" {
			b.WriteString("\n\n" + badStyle.Render(m.siteError))
		}
		b.WriteString("\n\n" + mutedStyle.Render("enter"))
		if m.cfg.Project != "" {
			b.WriteString(mutedStyle.Render("  esc"))
		} else {
			b.WriteString(mutedStyle.Render("  esc account"))
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

	if m.state.VMExists && len(m.steps) == 0 && strings.EqualFold(m.state.Instance.Status, "RUNNING") {
		if idx := firstMissingStep(m.state, m.cfg); idx >= 0 {
			steps := makeSteps(m.cfg.domainFor(m.state.Account) != "")
			b.WriteString(warnStyle.Render("Setup incomplete"))
			if idx < len(steps) {
				b.WriteString("  " + mutedStyle.Render(steps[idx].Name))
			}
			b.WriteString("\n\n" + button("Continue setup"))
			b.WriteString("\n\n" + mutedStyle.Render("enter  a account  r refresh  q"))
			return b.String()
		}
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
			hint := "r retry  d details  q"
			if errors.Is(m.lastErr, errDNSRequired) {
				b.WriteString("\n" + warnStyle.Render("A "+m.cfg.domainFor(m.state.Account)+" → "+m.state.StaticIP))
				hint = "r retry  s HTTP for now  d details  q"
			} else if m.cfg.domainFor(m.state.Account) != "" && m.stepIndex == 13 {
				hint = "r retry  s HTTP for now  d details  q"
			}
			b.WriteString("\n\n" + mutedStyle.Render(hint))
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
		ok, warn     bool
	}{
		{"VM", status, m.state.VMExists, false},
		{"IP", ip, ip != "", false},
		{"Hermes", chatGPTModel + " · " + chatGPTEffort, m.state.ChatGPTReady, false},
		{"GitHub", m.cfg.repoFor(m.state.Account), m.state.GitHubReady, false},
	}
	domain := m.cfg.domainFor(m.state.Account)
	if domain != "" {
		detail := domain
		warn := false
		if !m.state.HTTPSReady {
			detail = "HTTP only · " + domain
			warn = true
		}
		rows = append(rows, struct {
			name, detail string
			ok, warn     bool
		}{"HTTPS", detail, m.state.HTTPSReady, warn})
	} else {
		rows = append(rows, struct {
			name, detail string
			ok, warn     bool
		}{"Web", "HTTP · no domain", m.state.WebReady, false})
	}
	if status == "RUNNING" {
		backup, fresh := backupStatus(m.state.BackupTime)
		rows = append(rows, struct {
			name, detail string
			ok, warn     bool
		}{"Backup", backup, fresh, !fresh})
	}
	for _, r := range rows {
		icon := mutedStyle.Render("·")
		if r.warn {
			icon = warnStyle.Render("!")
		} else if r.ok {
			icon = goodStyle.Render("✓")
		}
		b.WriteString(fmt.Sprintf("%s %-8s", icon, r.name))
		if r.detail != "" {
			b.WriteString(" " + mutedStyle.Render(r.detail))
		}
		b.WriteString("\n")
	}
	if len(m.state.CostWarnings) > 0 {
		b.WriteString("\n" + warnStyle.Render("⚠ Potential billing") + "\n")
		for _, warning := range m.state.CostWarnings {
			b.WriteString(mutedStyle.Render("  "+warning) + "\n")
		}
	}
	b.WriteString("\n")
	for i, item := range m.menu {
		b.WriteString(choiceLine(item, i == m.menuPos) + "\n")
	}
	if m.statusText != "" {
		b.WriteString("\n" + spinner(m.frame) + " " + m.statusText)
	}
	b.WriteString("\n" + mutedStyle.Render("↑/↓  enter  r refresh  q"))
	return b.String()
}

func backupStatus(value string) (string, bool) {
	if value == "" {
		return "never", false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "unknown", false
	}
	age := time.Since(t)
	if age < 0 {
		age = 0
	}
	fresh := age < 36*time.Hour
	if age < 24*time.Hour {
		return "today", fresh
	}
	days := int(age / (24 * time.Hour))
	if days < 1 {
		days = 1
	}
	return fmt.Sprintf("%dd ago", days), fresh
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
	m.editingDomain, m.domainInput, m.domainError = false, "", ""
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
