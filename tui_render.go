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
		b.WriteString(accentStyle.Render("› ") + renderTextInput(m.domainInput, m.domainCursor))
		if m.domainError != "" {
			b.WriteString("\n\n" + badStyle.Render(m.domainError))
		}
		b.WriteString("\n\n" + hintLine("Enter", "save", "Esc", "cancel"))
		return b.String()
	}

	if m.editingSite {
		b.WriteString(titleStyle.Render("Domain / name") + "\n\n")
		b.WriteString(accentStyle.Render("› ") + renderTextInput(m.siteInput, m.siteCursor))
		if m.siteError != "" {
			b.WriteString("\n\n" + badStyle.Render(m.siteError))
		}
		if m.cfg.Project == "" {
			b.WriteString("\n\n" + hintLine("Enter", "save", "Esc", "accounts"))
		} else {
			b.WriteString("\n\n" + hintLine("Enter", "save", "Esc", "cancel"))
		}
		return b.String()
	}

	if m.refreshing && !m.cloudLoaded {
		b.WriteString(spinner(m.frame) + " " + mutedStyle.Render("Checking cloud"))
		b.WriteString("\n\n" + hintLine("A", "accounts", "Q", "quit"))
		return b.String()
	}

	if (!m.state.VMExists && len(m.steps) == 0) || m.cfg.disabledFor(m.state.Account) {
		if m.vmScanAccount != m.state.Account {
			b.WriteString(spinner(m.frame) + " " + mutedStyle.Render("Checking existing VMs"))
			b.WriteString("\n\n" + hintLine("A", "accounts", "Q", "quit"))
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
			b.WriteString("\n\n" + hintLine("Enter", "acknowledge", "A", "accounts", "Q", "quit"))
			return b.String()
		}
		b.WriteString(button("Create"))
		b.WriteString("\n\n" + hintLine("Enter", "create", "A", "accounts", "Q", "quit"))
		return b.String()
	}

	if m.state.VMExists && len(m.steps) == 0 && strings.EqualFold(m.state.Instance.Status, "RUNNING") && m.servicesLoaded {
		if idx := firstMissingStep(m.state, m.cfg); idx >= 0 {
			steps := makeSteps(m.cfg.domainFor(m.state.Account) != "")
			b.WriteString(warnStyle.Render("Setup incomplete"))
			if idx < len(steps) {
				b.WriteString("  " + mutedStyle.Render(steps[idx].Name))
			}
			b.WriteString("\n\n" + button("Continue setup"))
			b.WriteString("\n\n" + hintLine("Enter", "continue", "A", "accounts"))
			b.WriteString("\n" + hintLine("R", "refresh", "Q", "quit"))
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
			if errors.Is(m.lastErr, errDNSRequired) {
				b.WriteString("\n" + warnStyle.Render("A "+m.cfg.domainFor(m.state.Account)+" → "+m.state.StaticIP))
				b.WriteString("\n\n" + hintLine("R", "retry", "S", "HTTP for now"))
				b.WriteString("\n" + hintLine("D", "details", "A", "accounts", "Q", "quit"))
			} else if m.cfg.domainFor(m.state.Account) != "" && m.stepIndex == 13 {
				b.WriteString("\n\n" + hintLine("R", "retry", "S", "HTTP for now"))
				b.WriteString("\n" + hintLine("D", "details", "A", "accounts", "Q", "quit"))
			} else {
				b.WriteString("\n\n" + hintLine("R", "retry", "D", "details", "A", "accounts", "Q", "quit"))
			}
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

	type statusRow struct {
		name, detail string
		ok, warn     bool
	}
	rows := []statusRow{
		{"VM", status, m.state.VMExists, false},
		{"IP", ip, ip != "", false},
	}
	if m.servicesLoaded {
		rows = append(rows,
			statusRow{"Hermes", chatGPTModel + " · " + chatGPTEffort, m.state.HermesReady, false},
			statusRow{"GitHub", m.cfg.repoFor(m.state.Account), m.state.GitHubReady, false},
		)
	} else {
		rows = append(rows,
			statusRow{"Hermes", "checking…", false, false},
			statusRow{"GitHub", "checking…", false, false},
		)
	}

	domain := m.cfg.domainFor(m.state.Account)
	if domain != "" {
		if !m.servicesLoaded {
			rows = append(rows, statusRow{"HTTPS", "checking… · " + domain, false, false})
		} else {
			detail := domain
			warn := false
			if !m.state.HTTPSReady {
				warn = true
				if m.state.DNSReady {
					detail = "SSL pending · " + domain
				} else {
					detail = "pending DNS · " + domain
				}
			}
			rows = append(rows, statusRow{"HTTPS", detail, m.state.HTTPSReady, warn})
		}
	} else if m.servicesLoaded {
		rows = append(rows, statusRow{"Web", "HTTP · no domain", m.state.WebReady, false})
	} else {
		rows = append(rows, statusRow{"Web", "checking…", false, false})
	}
	if status == "RUNNING" {
		if m.servicesLoaded {
			backup, fresh := backupStatus(m.state.BackupTime)
			rows = append(rows, statusRow{"Backup", backup, fresh, !fresh})
		} else {
			rows = append(rows, statusRow{"Backup", "checking…", false, false})
		}
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
	if m.servicesLoaded && domain != "" && !m.state.HTTPSReady && !m.state.DNSReady && ip != "" {
		b.WriteString(mutedStyle.Render("  A "+domain+" → "+ip) + "\n")
	}
	if m.servicesLoaded && len(m.state.CostWarnings) > 0 {
		b.WriteString("\n" + warnStyle.Render("⚠ Potential billing") + "\n")
		for _, warning := range m.state.CostWarnings {
			b.WriteString(mutedStyle.Render("  "+warning) + "\n")
		}
	}
	b.WriteString("\n")
	for i, item := range m.menu {
		b.WriteString(choiceLine(item, i == m.menuPos) + "\n")
	}
	if m.refreshing {
		b.WriteString("\n" + spinner(m.frame) + " " + mutedStyle.Render("Refreshing cloud"))
	} else if m.servicesRefreshing {
		b.WriteString("\n" + spinner(m.frame) + " " + mutedStyle.Render("Checking services"))
	} else if m.statusText != "" {
		b.WriteString("\n" + mutedStyle.Render(m.statusText))
	}
	b.WriteString("\n" + hintLine("↑↓", "move", "Enter", "open"))
	b.WriteString("\n" + hintLine("A", "accounts", "R", "refresh", "Q", "quit"))
	return b.String()
}

func renderTextInput(value string, cursor int) string {
	r := []rune(value)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(r) {
		cursor = len(r)
	}
	return string(r[:cursor]) + accentStyle.Render("▌") + string(r[cursor:])
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
	b.WriteString("\n" + hintLine("↑↓", "move", "Enter", "select", "Esc", "back"))
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
	b.WriteString("\n\n" + strings.Join(lines, "\n") + "\n\n" + hintLine("Enter/Esc", "back", "A", "accounts"))
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
	m.siteCursor = 0
	m.editingDomain, m.domainInput, m.domainError = false, "", ""
	m.domainCursor = 0
	m.steps, m.stepIndex = nil, 0
	m.lastErr, m.lastOutput, m.lastCommand = nil, "", ""
	m.refreshing, m.servicesRefreshing = false, false
	m.cloudLoaded, m.servicesLoaded = false, false
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
