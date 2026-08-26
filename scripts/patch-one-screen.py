from pathlib import Path

p = Path("main.go")
s = p.read_text()

s = s.replace(
    '\t"regexp"\n\t"runtime"\n\t"strings"\n\t"time"\n',
    '\t"regexp"\n\t"runtime"\n\t"sort"\n\t"strings"\n\t"sync"\n\t"time"\n',
    1,
)

old = '''type cloudState struct {
\tGcloud      bool
\tAccount     string
\tBilling     []billingAccount
\tProjectOK   bool
\tVMExists    bool
\tInstance    instanceInfo
\tStaticIP    string
\tHermesReady bool
}
'''
new = old + '''
type existingVM struct {
\tProject string
\tName    string
\tZone    string
\tStatus  string
}
'''
assert old in s
s = s.replace(old, new, 1)

old = '''\tbilling    []billingAccount
\tbillingPos int
\tbillingID  string
\tsignInPos  int
'''
new = old + '''
\totherVMs      []existingVM
\totherVMCount  int
\tvmScanAccount string
\tvmScanBusy    bool
\tvmWarningAck  bool
'''
assert old in s
s = s.replace(old, new, 1)

old = '''type detectedMsg struct {
\tstate cloudState
\terr   error
}
'''
new = old + '''
type vmScanMsg struct {
\taccount string
\tvms     []existingVM
\tcount   int
}
'''
assert old in s
s = s.replace(old, new, 1)

old = '''\t\tpreviousAccount := m.state.Account
\t\tif previousAccount != msg.state.Account {
\t\t\tm.billingID = ""
\t\t\tm.billingPos = 0
\t\t}
\t\tif m.billingID != "" && !billingListContains(msg.state.Billing, m.billingID) {
\t\t\tm.billingID = ""
\t\t\tm.billingPos = 0
\t\t}
\t\tm.state = msg.state
\t\tm.billing = msg.state.Billing
\t\tm.cfg.Account = msg.state.Account
\t\tm.cfg.Project = m.cfg.projectFor(msg.state.Account)
\t\tm.lastErr = nil
\t\tm.routeAfterDetect()
\t\treturn m, nil
'''
new = '''\t\tpreviousAccount := m.state.Account
\t\tif previousAccount != msg.state.Account {
\t\t\tm.billingID = ""
\t\t\tm.billingPos = 0
\t\t\tm.otherVMs = nil
\t\t\tm.otherVMCount = 0
\t\t\tm.vmScanAccount = ""
\t\t\tm.vmScanBusy = false
\t\t\tm.vmWarningAck = false
\t\t}
\t\tif m.billingID != "" && !billingListContains(msg.state.Billing, m.billingID) {
\t\t\tm.billingID = ""
\t\t\tm.billingPos = 0
\t\t}
\t\tm.state = msg.state
\t\tm.billing = msg.state.Billing
\t\tm.cfg.Account = msg.state.Account
\t\tm.cfg.Project = m.cfg.projectFor(msg.state.Account)
\t\tm.lastErr = nil

\t\tvar cmds []tea.Cmd
\t\tif cmd := m.routeAfterDetect(); cmd != nil {
\t\t\tcmds = append(cmds, cmd)
\t\t}
\t\tif m.state.Account != "" && !m.vmScanBusy && m.vmScanAccount != m.state.Account {
\t\t\tm.vmScanBusy = true
\t\t\tcmds = append(cmds, scanVMsCmd(m.state.Account, m.cfg.Project))
\t\t}
\t\tif len(cmds) == 0 {
\t\t\treturn m, nil
\t\t}
\t\treturn m, tea.Batch(cmds...)

\tcase vmScanMsg:
\t\tm.vmScanBusy = false
\t\tif msg.account != m.state.Account {
\t\t\treturn m, nil
\t\t}
\t\tm.otherVMs = msg.vms
\t\tm.otherVMCount = msg.count
\t\tm.vmScanAccount = msg.account
\t\treturn m, m.routeAfterDetect()
'''
assert old in s
s = s.replace(old, new, 1)

old = '''\t\tcase "enter":
\t\t\tif len(m.billing) > 0 {
\t\t\t\tm.billingID = m.billing[m.billingPos].id()
\t\t\t\tm.screen = screenCreate
\t\t\t}
'''
new = '''\t\tcase "enter":
\t\t\tif len(m.billing) > 0 {
\t\t\t\tm.billingID = m.billing[m.billingPos].id()
\t\t\t\treturn m, m.routeAfterDetect()
\t\t\t}
'''
assert old in s
s = s.replace(old, new, 1)

old = '''\tcase screenCreate:
\t\tswitch k {
\t\tcase "enter":
\t\t\tif m.billingID == "" && len(m.billing) == 1 {
\t\t\t\tm.billingID = m.billing[0].id()
\t\t\t}
\t\t\tif m.billingID == "" {
\t\t\t\tm.screen = screenBillingPick
\t\t\t\treturn m, nil
\t\t\t}
\t\t\tm.startProvision()
\t\t\treturn m, runProvisionStepCmd(0, m.cfg, m.billingID)
\t\tcase "a":
\t\t\tm.busy = true
\t\t\treturn m, authCmd()
\t\tcase "q":
\t\t\treturn m, tea.Quit
\t\t}
'''
new = '''\tcase screenCreate:
\t\tswitch k {
\t\tcase "enter":
\t\t\tif m.vmScanAccount != m.state.Account {
\t\t\t\treturn m, nil
\t\t\t}
\t\t\tif m.otherVMCount > 0 && !m.vmWarningAck {
\t\t\t\tm.vmWarningAck = true
\t\t\t}
\t\t\tif m.billingID == "" && len(m.billing) == 1 {
\t\t\t\tm.billingID = m.billing[0].id()
\t\t\t}
\t\t\tif m.billingID == "" {
\t\t\t\tm.screen = screenBillingPick
\t\t\t\treturn m, nil
\t\t\t}
\t\t\tm.startProvision()
\t\t\treturn m, runProvisionStepCmd(0, m.cfg, m.billingID)
\t\tcase "a":
\t\t\tm.busy = true
\t\t\treturn m, authCmd()
\t\tcase "q":
\t\t\treturn m, tea.Quit
\t\t}
'''
assert old in s
s = s.replace(old, new, 1)

old = '''func (m *model) routeAfterDetect() {
\tif !m.state.Gcloud {
\t\tm.screen = screenNeedGcloud
\t\treturn
\t}
\tif m.state.Account == "" {
\t\tm.screen = screenSignIn
\t\treturn
\t}
\tif m.cfg.Project != "" && m.state.VMExists {
\t\tm.screen = screenDashboard
\t\tm.syncMenu()
\t\treturn
\t}
\tif len(m.state.Billing) == 0 {
\t\tm.screen = screenBilling
\t\treturn
\t}
\tif len(m.state.Billing) == 1 {
\t\tm.billingID = m.state.Billing[0].id()
\t\tm.screen = screenCreate
\t\treturn
\t}
\tif m.billingID == "" {
\t\tm.screen = screenBillingPick
\t\treturn
\t}
\tm.screen = screenCreate
}
'''
new = '''func (m *model) routeAfterDetect() tea.Cmd {
\tif !m.state.Gcloud {
\t\tm.screen = screenNeedGcloud
\t\treturn nil
\t}
\tif m.state.Account == "" {
\t\tm.screen = screenSignIn
\t\treturn nil
\t}

\tif m.cfg.Project != "" && m.state.VMExists {
\t\tif strings.EqualFold(m.state.Instance.Status, "RUNNING") && !m.state.HermesReady {
\t\t\tm.startProvisionFrom(6)
\t\t\treturn runProvisionStepCmd(m.stepIndex, m.cfg, m.billingID)
\t\t}
\t\tm.screen = screenDashboard
\t\tm.syncMenu()
\t\treturn nil
\t}

\tif len(m.state.Billing) == 0 {
\t\tm.screen = screenBilling
\t\treturn nil
\t}
\tif len(m.state.Billing) == 1 {
\t\tm.billingID = m.state.Billing[0].id()
\t} else if m.billingID == "" {
\t\tm.screen = screenBillingPick
\t\treturn nil
\t}

\t// Before creating anything, finish the account-wide VM scan. Existing VMs
\t// pause automation on this same screen until Enter explicitly continues.
\tif m.vmScanAccount != m.state.Account || (m.otherVMCount > 0 && !m.vmWarningAck) {
\t\tm.screen = screenCreate
\t\treturn nil
\t}

\tm.startProvision()
\treturn runProvisionStepCmd(0, m.cfg, m.billingID)
}
'''
assert old in s
s = s.replace(old, new, 1)

marker = '''func (m *model) startProvision() {
\tm.screen = screenProvision
\tm.busy = true
\tm.lastErr = nil
\tm.lastOutput = ""
\tm.lastCommand = ""
\tm.stepIndex = 0
\tm.steps = []provisionStep{
\t\t{Name: "Project", State: 1},
\t\t{Name: "Billing"},
\t\t{Name: "Compute Engine"},
\t\t{Name: "Network"},
\t\t{Name: "Static IP"},
\t\t{Name: "Debian VM"},
\t\t{Name: "SSH"},
\t\t{Name: "Hermes"},
\t\t{Name: "Verify"},
\t}
}
'''
addition = marker + '''
func (m *model) startProvisionFrom(index int) {
\tm.startProvision()
\tif index < 0 {
\t\tindex = 0
\t}
\tif index >= len(m.steps) {
\t\tindex = len(m.steps) - 1
\t}
\tfor i := 0; i < index; i++ {
\t\tm.steps[i].State = 2
\t}
\tm.stepIndex = index
\tm.steps[index].State = 1
}
'''
assert marker in s
s = s.replace(marker, addition, 1)

old = '''\tcase screenCreate:
\t\treturn m.renderCreate()
\tcase screenProvision:
\t\treturn m.renderProvision()
\tcase screenDashboard:
\t\treturn m.renderDashboard()
'''
new = '''\tcase screenCreate, screenProvision, screenDashboard:
\t\treturn m.renderServer()
'''
assert old in s
s = s.replace(old, new, 1)

render_server = r'''func (m model) renderServer() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("Server"))
	if m.state.Account != "" {
		b.WriteString("  " + mutedStyle.Render(m.state.Account))
	}
	b.WriteString("\n\n")

	if m.otherVMCount > 0 {
		label := fmt.Sprintf("⚠ %d other VM", m.otherVMCount)
		if m.otherVMCount != 1 {
			label += "s"
		}
		b.WriteString(warnStyle.Render(label))
		if len(m.otherVMs) > 0 {
			v := m.otherVMs[0]
			b.WriteString(mutedStyle.Render("  " + v.Project + "/" + v.Name))
			if m.otherVMCount > 1 {
				b.WriteString(mutedStyle.Render(fmt.Sprintf("  +%d", m.otherVMCount-1)))
			}
		}
		b.WriteString("\n\n")
	}

	if m.screen == screenCreate {
		if m.vmScanAccount != m.state.Account {
			b.WriteString(spinner(m.frame) + " instances\n")
		} else {
			for _, name := range []string{"Project", "Billing", "Compute", "Network", "Static IP", "VM", "SSH", "Hermes", "Verify"} {
				b.WriteString(mutedStyle.Render("·") + " " + name + "\n")
			}
		}
		if m.otherVMCount > 0 && !m.vmWarningAck {
			b.WriteString("\n" + mutedStyle.Render("enter continue  a account  q"))
		}
		return b.String()
	}

	if m.screen == screenProvision {
		for _, step := range m.steps {
			icon := mutedStyle.Render("·")
			switch step.State {
			case 1:
				icon = accentStyle.Render(spinner(m.frame))
			case 2:
				icon = goodStyle.Render("✓")
			case 3:
				icon = badStyle.Render("✕")
			}
			line := fmt.Sprintf("%s %-15s", icon, step.Name)
			if step.Detail != "" {
				line += " " + mutedStyle.Render(step.Detail)
			}
			b.WriteString(line + "\n")
		}
		if !m.busy && m.stepIndex < len(m.steps) && m.steps[m.stepIndex].State == 3 {
			b.WriteString("\n" + badStyle.Render(shortError(m.lastErr)) + "\n\n")
			b.WriteString(mutedStyle.Render("r retry  d details"))
			if hasExecutable("pi") {
				b.WriteString(mutedStyle.Render("  p Pi"))
			}
		}
		return b.String()
	}

	ip := m.state.Instance.ip()
	if ip == "" {
		ip = m.state.StaticIP
	}
	if ip == "" {
		ip = "—"
	}
	status := strings.ToUpper(m.state.Instance.Status)
	if status == "" {
		status = "—"
	}
	rows := []struct {
		name   string
		detail string
		done   bool
	}{
		{"Project", m.cfg.Project, m.cfg.Project != "" && m.state.ProjectOK},
		{"Billing", "", true},
		{"Compute", "", m.state.VMExists},
		{"Network", "", m.state.VMExists},
		{"Static IP", m.state.StaticIP, m.state.StaticIP != ""},
		{"VM", status, m.state.VMExists},
		{"SSH", "", m.state.HermesReady},
		{"Hermes", "", m.state.HermesReady},
		{"Verify", "", m.state.HermesReady},
	}
	for _, row := range rows {
		icon := mutedStyle.Render("·")
		if row.done {
			icon = goodStyle.Render("✓")
		}
		line := fmt.Sprintf("%s %-15s", icon, row.name)
		if row.detail != "" {
			line += " " + mutedStyle.Render(row.detail)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + titleStyle.Render(ip) + "\n\n")
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

'''
marker = 'func (m model) renderConfirm() string {'
assert marker in s
s = s.replace(marker, render_server + marker, 1)

old = '''\tif err == nil && strings.TrimSpace(res.Stdout) != "" {
\t\ts.VMExists = true
\t\t_ = json.Unmarshal([]byte(res.Stdout), &s.Instance)
\t}

\tres, err = run(ctx, "gcloud", "compute", "addresses", "describe", addressName,
'''
new = '''\tif err == nil && strings.TrimSpace(res.Stdout) != "" {
\t\ts.VMExists = true
\t\t_ = json.Unmarshal([]byte(res.Stdout), &s.Instance)
\t\tif strings.EqualFold(s.Instance.Status, "RUNNING") {
\t\t\tprobeCtx, probeCancel := context.WithTimeout(context.Background(), 12*time.Second)
\t\t\t_, probeErr := run(probeCtx, "gcloud", "compute", "ssh", vmName,
\t\t\t\t"--project="+project, "--zone="+zone,
\t\t\t\t"--command=sudo -n test -x /usr/local/bin/hermes", "--quiet")
\t\t\tprobeCancel()
\t\t\ts.HermesReady = probeErr == nil
\t\t}
\t}

\tres, err = run(ctx, "gcloud", "compute", "addresses", "describe", addressName,
'''
assert old in s
s = s.replace(old, new, 1)

scan_code = r'''func scanVMsCmd(account, managedProject string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		vms, count := scanExistingVMs(ctx, managedProject)
		return vmScanMsg{account: account, vms: vms, count: count}
	}
}

func scanExistingVMs(ctx context.Context, managedProject string) ([]existingVM, int) {
	res, err := run(ctx, "gcloud", "projects", "list",
		"--filter=lifecycleState:ACTIVE", "--format=value(projectId)")
	if err != nil {
		return nil, 0
	}
	projects := nonEmptyLines(res.Stdout)
	if len(projects) == 0 {
		return nil, 0
	}

	type scanResult struct{ vms []existingVM }
	results := make(chan scanResult, len(projects))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, project := range projects {
		project := project
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			vmRes, err := run(ctx, "gcloud", "compute", "instances", "list",
				"--project="+project,
				"--format=value(name,zone.basename(),status)")
			if err != nil {
				return
			}
			var found []existingVM
			for _, line := range nonEmptyLines(vmRes.Stdout) {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				name := fields[0]
				if project == managedProject && name == vmName {
					continue
				}
				vm := existingVM{Project: project, Name: name}
				if len(fields) > 1 {
					vm.Zone = fields[1]
				}
				if len(fields) > 2 {
					vm.Status = fields[2]
				}
				found = append(found, vm)
			}
			if len(found) > 0 {
				select {
				case results <- scanResult{vms: found}:
				case <-ctx.Done():
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var all []existingVM
	for result := range results {
		all = append(all, result.vms...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Project == all[j].Project {
			return all[i].Name < all[j].Name
		}
		return all[i].Project < all[j].Project
	})
	count := len(all)
	if len(all) > 3 {
		all = all[:3]
	}
	return all, count
}

'''
marker = 'func authCmd() tea.Cmd {'
assert marker in s
s = s.replace(marker, scan_code + marker, 1)

p.write_text(s)
